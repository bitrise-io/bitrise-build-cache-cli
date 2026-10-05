// Package dsymshim implements the dsymutil CAS-plugin shim that rewrites argv
// to pass -cas-plugin-path + -cas, then execs the stock toolchain dsymutil.
//
// Context: swiftc under -cache-compile-job embeds CAS ids in .o files; the
// GenerateDSYMFile task invokes /usr/bin/dsymutil with no CAS wiring and prints
// `0~<base64>==: No such file or directory` for each embedded id, producing a
// smaller, incomplete dSYM. The shim closes that gap at the dsymutil layer.
package dsymshim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// EnvKillSwitch disables the shim's argv-rewrite path at runtime. When set to any
// non-empty value the shim execs the stock dsymutil with the original argv.
// Documented alongside the activate-time killswitch so operators can roll back
// per-invocation without reactivating.
const EnvKillSwitch = "BITRISE_BUILD_CACHE_SKIP_DSYMUTIL_SHIM"

// EnvSessionID scopes touch-file writes + drains to one xcodebuild wrapper run,
// so concurrent xcodebuilds on the same host do not read-then-delete each
// other's touch files. The wrapper injects it on the xcodebuild subprocess env;
// shim invocations inherit it transitively through the toolchain trampoline.
const EnvSessionID = "BITRISE_BUILD_CACHE_DSYMSHIM_SESSION_ID"

// setupBudget is the hard upper bound for all pre-exec work (resolve, feature
// detect, filter plumbing). If we overrun, we bypass to stock dsymutil to keep
// the archive wall-clock honest.
const setupBudget = 100 * time.Millisecond

// SummaryToken is the grep anchor for the shim's end-of-run summary line (one
// per invocation on stderr + mirrored to the touch file).
const SummaryToken = "dsymutil-shim" //nolint:gosec // operator-visible marker, not a credential

// Params configures a single shim invocation. Nil-ok fields fall back to
// production defaults; tests inject fakes.
type Params struct {
	Argv   []string
	Env    map[string]string
	Stdin  *os.File
	Stdout *os.File
	Stderr *os.File

	Logger  log.Logger
	OsProxy utils.OsProxy
	Paths   paths.Paths

	// StockDsymutilPath lets tests point exec at a fake binary without
	// resolving via DEVELOPER_DIR. Zero value triggers DEVELOPER_DIR + fallback
	// to /usr/bin/dsymutil resolution.
	StockDsymutilPath string

	// Clock lets tests fake the setup-budget wall clock.
	Clock func() time.Time

	// FeatureProbe lets tests stub dsymutil --help feature detection.
	// Returns (supportsCAS, versionKey, err). versionKey is the cache key the
	// probe keys its answer by; implementations typically embed dsymutil --version.
	FeatureProbe func(ctx context.Context, dsymutilPath string) (bool, string, error)

	// ExecCommand returns an *exec.Cmd for the inner dsymutil run. Tests inject
	// a fake that observes argv and returns a deterministic exit code.
	ExecCommand func(ctx context.Context, name string, argv ...string) *exec.Cmd
}

// touchRecord is one line of ~/.local/state/xcelerate/dsymshim/<session>-<pid>-<ns>.ndjson.
// The wrapper drains + aggregates these at end-of-xcodebuild.
type touchRecord struct {
	Timestamp      time.Time `json:"timestamp"`
	MachOPath      string    `json:"machoPath,omitempty"`
	Mode           string    `json:"mode"`
	MissedCount    int       `json:"missed"`
	FilteredStderr int       `json:"filteredStderr"`
	DurationMillis int64     `json:"durationMs"`
	ExitCode       int       `json:"exitCode"`
	PluginPath     string    `json:"pluginPath,omitempty"`
	CasPath        string    `json:"casPath,omitempty"`
	StockDsymutil  string    `json:"stockDsymutil,omitempty"`
	Note           string    `json:"note,omitempty"`
}

// Run is the shim's entry point. Returns the exit code the caller should surface
// to the xcodebuild dispatcher.
func (p Params) Run(ctx context.Context) int {
	start := p.now()
	logger := p.Logger
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(p.Stderr, "[%s] panic recovered: %v\n", SummaryToken, r)
		}
	}()

	osProxy := p.OsProxy
	if osProxy == nil {
		osProxy = utils.DefaultOsProxy{}
	}

	// Kill switch: active regardless of other state. Rolls the invocation back
	// to a plain stock-dsymutil exec with the user's original argv.
	if v := p.Env[EnvKillSwitch]; v != "" {
		return p.bypass(ctx, osProxy, start, "killswitch")
	}

	// Idempotency: swiftbuild (or a repeat shim) may already have the flags on
	// argv. Avoid re-prepending.
	if argvHasCASFlags(p.Argv) {
		return p.bypass(ctx, osProxy, start, "already-wired")
	}

	stockPath := p.resolveStockDsymutil()

	// Feature detect (per dsymutil --version), cached across runs. Any error -> bypass.
	supportsCAS, _, err := p.probeCASSupport(ctx, stockPath)
	if err != nil || !supportsCAS {
		if err != nil {
			logger.Debugf("[%s] feature probe failed: %v", SummaryToken, err)
		}

		return p.bypass(ctx, osProxy, start, "no-cas-support")
	}

	pluginPath, casPath, resolveNote := Resolve(p.Argv, p.Env, osProxy)

	// A hard overrun on setup is almost always a sign of a slow fs probe or a
	// Spotlight hiccup; bypass rather than block the user's archive on it.
	if p.elapsed(start) > setupBudget {
		return p.bypass(ctx, osProxy, start, "setup-slow")
	}

	rewritten := prependCASFlags(p.Argv, pluginPath, casPath)
	logger.Debugf("[%s] running %s %s", SummaryToken, stockPath, strings.Join(rewritten, " "))

	exit, counts := p.runStock(ctx, stockPath, rewritten)

	summary := fmt.Sprintf("[%s] missed %d CAS ids (filtered %d stderr lines)",
		SummaryToken, counts.Missed, counts.FilteredStderr)
	fmt.Fprintln(p.Stderr, summary)

	rec := touchRecord{
		Timestamp:      p.now(),
		MachOPath:      firstPositional(p.Argv),
		Mode:           "shim",
		MissedCount:    counts.Missed,
		FilteredStderr: counts.FilteredStderr,
		DurationMillis: p.elapsed(start).Milliseconds(),
		ExitCode:       exit,
		PluginPath:     pluginPath,
		CasPath:        casPath,
		StockDsymutil:  stockPath,
		Note:           resolveNote,
	}
	p.writeTouch(osProxy, rec)

	return exit
}

type counts struct {
	Missed         int
	FilteredStderr int
}

func (p Params) runStock(ctx context.Context, stockPath string, argv []string) (int, counts) {
	execFn := p.ExecCommand
	if execFn == nil {
		execFn = exec.CommandContext
	}

	cmd := execFn(ctx, stockPath, argv...)
	cmd.Stdin = p.Stdin
	cmd.Stdout = p.Stdout
	// Tap stderr through the filter so we can both strip the paired lines and count them.
	filter := NewStderrFilter(p.Stderr)
	cmd.Stderr = filter

	err := cmd.Run()
	exit := exitCodeFrom(err)

	flushed := filter.Close()

	// Missed is the only ground-truth signal dsymutil emits: one paired warning
	// per CAS id the plugin failed to resolve. Missed==0 means every id resolved.
	return exit, counts{
		Missed:         flushed.FilteredPaired,
		FilteredStderr: flushed.FilteredPaired * 2,
	}
}

// Private — helpers

func (p Params) bypass(ctx context.Context, osProxy utils.OsProxy, start time.Time, reason string) int {
	stockPath := p.resolveStockDsymutil()

	execFn := p.ExecCommand
	if execFn == nil {
		execFn = exec.CommandContext
	}

	cmd := execFn(ctx, stockPath, p.Argv...)
	cmd.Stdin = p.Stdin
	cmd.Stdout = p.Stdout
	cmd.Stderr = p.Stderr

	err := cmd.Run()
	exit := exitCodeFrom(err)

	rec := touchRecord{
		Timestamp:      p.now(),
		MachOPath:      firstPositional(p.Argv),
		Mode:           "bypass:" + reason,
		DurationMillis: p.elapsed(start).Milliseconds(),
		ExitCode:       exit,
		StockDsymutil:  stockPath,
	}
	p.writeTouch(osProxy, rec)

	return exit
}

func (p Params) now() time.Time {
	if p.Clock != nil {
		return p.Clock()
	}

	return time.Now()
}

func (p Params) elapsed(start time.Time) time.Duration {
	return p.now().Sub(start)
}

func (p Params) resolveStockDsymutil() string {
	if p.StockDsymutilPath != "" {
		return p.StockDsymutilPath
	}

	if dev := p.Env["DEVELOPER_DIR"]; dev != "" {
		candidate := filepath.Join(dev, "Toolchains", "XcodeDefault.xctoolchain", "usr", "bin", "dsymutil")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return "/usr/bin/dsymutil"
}

var (
	//nolint:gochecknoglobals // process-local cache keyed by dsymutil --version output
	featureCache = map[string]bool{}
	//nolint:gochecknoglobals
	featureCacheLock sync.Mutex
)

func (p Params) probeCASSupport(ctx context.Context, dsymutilPath string) (bool, string, error) {
	if p.FeatureProbe != nil {
		return p.FeatureProbe(ctx, dsymutilPath)
	}

	versionKey, err := dsymutilVersionKey(ctx, dsymutilPath)
	if err != nil {
		return false, "", err
	}

	featureCacheLock.Lock()
	cached, ok := featureCache[versionKey]
	featureCacheLock.Unlock()
	if ok {
		return cached, versionKey, nil
	}

	helpCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	out, err := exec.CommandContext(helpCtx, dsymutilPath, "--help").CombinedOutput()
	if err != nil {
		return false, versionKey, fmt.Errorf("dsymutil --help: %w", err)
	}

	supports := strings.Contains(string(out), "-cas-plugin-path")

	featureCacheLock.Lock()
	featureCache[versionKey] = supports
	featureCacheLock.Unlock()

	return supports, versionKey, nil
}

func dsymutilVersionKey(ctx context.Context, dsymutilPath string) (string, error) {
	verCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	out, err := exec.CommandContext(verCtx, dsymutilPath, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("dsymutil --version: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

func (p Params) writeTouch(osProxy utils.OsProxy, rec touchRecord) {
	dir := p.Paths.XcelerateDsymShimDir()
	if err := osProxy.MkdirAll(dir, 0o755); err != nil {
		p.debugf("mkdir touch dir %s: %v", dir, err)

		return
	}

	session := p.Env[EnvSessionID]
	if session == "" {
		session = "nosession"
	}

	name := fmt.Sprintf("%s-%d-%d.ndjson", session, os.Getpid(), rec.Timestamp.UnixNano())
	path := filepath.Join(dir, name)

	data, err := json.Marshal(rec)
	if err != nil {
		p.debugf("marshal touch record: %v", err)

		return
	}
	data = append(data, '\n')

	if err := osProxy.WriteFile(path, data, 0o644); err != nil {
		p.debugf("write touch %s: %v", path, err)
	}
}

func (p Params) debugf(format string, args ...any) {
	if p.Logger != nil {
		p.Logger.Debugf("["+SummaryToken+"] "+format, args...)
	}
}

func exitCodeFrom(err error) int {
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}

	return 1
}

func argvHasCASFlags(argv []string) bool {
	for i, a := range argv {
		if a == "-cas-plugin-path" || a == "--cas-plugin-path" {
			return true
		}
		if strings.HasPrefix(a, "-cas-plugin-path=") || strings.HasPrefix(a, "--cas-plugin-path=") {
			return true
		}
		_ = i
	}

	return false
}

// prependCASFlags inserts the -cas-plugin-path and (when set) -cas flags at the
// front of argv so they bind before any user-supplied conflicting flag.
func prependCASFlags(argv []string, pluginPath, casPath string) []string {
	if pluginPath == "" {
		return slices.Clone(argv)
	}

	out := make([]string, 0, len(argv)+4)
	out = append(out, "-cas-plugin-path", pluginPath)
	if casPath != "" {
		out = append(out, "-cas", casPath)
	}
	out = append(out, argv...)

	return out
}

func firstPositional(argv []string) string {
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if !strings.HasPrefix(a, "-") {
			return a
		}
		if a == "-o" || a == "--out" || a == "-oso-prepend-path" {
			i++ // skip value
		}
	}

	return ""
}
