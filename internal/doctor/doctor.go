// Package doctor implements the diagnostic + repair flow that backs the
// `bitrise-build-cache doctor` subcommand.
//
// Doctor merges what used to be two separate commands (`health` for a fast
// status view, `doctor` for smoke tests). The team's view was that the split
// added user-mental-load without saving meaningful runtime — most checks are
// sub-millisecond. The single doctor:
//
//   - Runs every check on each invocation.
//   - Applies safe repairs when --fix is set.
//   - Skips the only slow probe (GitHub release lookup) with --no-update-check.
package doctor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/auth/keychain"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

// State is the severity of a single Check result.
type State string

const (
	StateOK    State = "ok"
	StateWarn  State = "warn"
	StateError State = "error"
)

// Result is the outcome of a single Diagnose call.
type Result struct {
	State   State  `json:"state"`
	Detail  string `json:"detail"`
	Fixable bool   `json:"fixable"`
}

// Check is one diagnostic + optional repair.
type Check struct {
	Name     string                               `json:"name"`
	Diagnose func(context.Context) Result         `json:"-"`
	Fix      func() (fixDetail string, err error) `json:"-"`
}

// Report is the full doctor output.
type Report struct {
	Items   []ReportItem `json:"items"`
	Version string       `json:"cli_version"`
}

// ReportItem is the outcome for one check, after optional fix attempt.
type ReportItem struct {
	Name      string  `json:"name"`
	Result    Result  `json:"result"`
	FixResult *string `json:"fix_result,omitempty"`
	FixError  string  `json:"fix_error,omitempty"`
}

// Overall returns the worst state across the report.
func (r Report) Overall() State {
	worst := StateOK
	for _, it := range r.Items {
		switch it.Result.State {
		case StateError:
			return StateError
		case StateWarn:
			worst = StateWarn
		case StateOK:
		}
	}

	return worst
}

// Options controls doctor's behaviour.
type Options struct {
	ApplyFixes      bool
	SkipUpdateCheck bool
}

// authLoader matches *keychain.Keychain. DI for tests.
type authLoader interface {
	Load() (keychain.Credentials, error)
}

// keyringBackend is the low-level go-keyring slice we use for the keychain
// smoke test. Tests inject an in-memory implementation.
type keyringBackend interface {
	Set(service, account, secret string) error
	Get(service, account string) (string, error)
	Delete(service, account string) error
}

// Runner aggregates all checks. Each field is injectable for hermetic tests;
// nil means "use the default".
type Runner struct {
	OsProxy            utils.OsProxy
	Envs               map[string]string
	CLIVersion         string
	HTTPClient         *http.Client
	AuthLoader         authLoader
	Keyring            keyringBackend
	XcelerateProxyDir  func() string
	LookPath           func(string) (string, error)
	StateDirCandidates []string
	LatestReleaseTag   func(ctx context.Context, c *http.Client) (string, error)
}

// NewRunner returns a Runner with production defaults.
func NewRunner() *Runner {
	osProxy := utils.DefaultOsProxy{}

	return &Runner{
		OsProxy:            osProxy,
		Envs:               utils.AllEnvs(),
		CLIVersion:         common.GetCLIVersion(nil),
		HTTPClient:         &http.Client{Timeout: 3 * time.Second},
		AuthLoader:         keychain.New(),
		Keyring:            realKeyringBackend{},
		XcelerateProxyDir:  func() string { return xcelerate.DirPath(osProxy) },
		LookPath:           exec.LookPath,
		StateDirCandidates: []string{"~/.local/state/xcelerate/logs", "~/.local/state/ccache/logs"},
		LatestReleaseTag:   fetchLatestGitHubRelease,
	}
}

type realKeyringBackend struct{}

func (realKeyringBackend) Set(service, account, secret string) error {
	return keyring.Set(service, account, secret) //nolint:wrapcheck
}

func (realKeyringBackend) Get(service, account string) (string, error) {
	return keyring.Get(service, account) //nolint:wrapcheck
}

func (realKeyringBackend) Delete(service, account string) error {
	return keyring.Delete(service, account) //nolint:wrapcheck
}

// Run runs every check + optionally applies fixes for fixable items.
func (r *Runner) Run(ctx context.Context, opts Options) Report {
	checks := r.checks(opts.SkipUpdateCheck)
	items := make([]ReportItem, 0, len(checks))

	for _, c := range checks {
		res := c.Diagnose(ctx)
		item := ReportItem{Name: c.Name, Result: res}

		if opts.ApplyFixes && res.Fixable && c.Fix != nil {
			detail, fxerr := c.Fix()
			if fxerr != nil {
				item.FixError = fxerr.Error()
			} else {
				item.FixResult = &detail
			}
		}

		items = append(items, item)
	}

	return Report{Items: items, Version: r.CLIVersion}
}

func (r *Runner) checks(skipUpdateCheck bool) []Check {
	checks := []Check{
		r.authCheck(),
		r.keychainSmokeCheck(),
		r.xcelerateProxyCheck(),
		r.ccacheHelperCheck(),
		r.ccacheBinaryCheck(),
		r.logDirsCheck(),
	}

	if !skipUpdateCheck {
		checks = append(checks, r.cliVersionCheck())
	}

	return checks
}

// ──────────────────────────── auth + keychain ────────────────────────────

func (r *Runner) authCheck() Check {
	return Check{
		Name: "auth",
		Diagnose: func(_ context.Context) Result {
			if r.AuthLoader != nil {
				if creds, err := r.AuthLoader.Load(); err == nil && creds.AuthToken != "" && creds.WorkspaceID != "" {
					return Result{State: StateOK, Detail: "OS keychain, workspace=" + creds.WorkspaceID}
				}
			}

			if cfg, err := common.ReadAuthConfigFromEnvironments(r.Envs); err == nil {
				return Result{State: StateOK, Detail: "environment variables, workspace=" + cfg.WorkspaceID}
			}

			return Result{
				State:  StateError,
				Detail: "no credentials found. Run `bitrise-build-cache auth set --token … --workspace-id …` or `bitrise-build-cache activate --interactive`.",
			}
		},
	}
}

const (
	smokeServiceName = "bitrise-build-cache-doctor"
	smokeAccountName = "smoketest"
)

// newSmokeSecret returns a per-run nonce so a leftover entry from a previous
// run (whose Delete failed) can't masquerade as a successful round-trip and
// hide a backend bug.
func newSmokeSecret() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read should never fail in practice; fall back to a deterministic
		// value so we still produce *some* secret rather than crash.
		return "smoketest-fallback"
	}

	return "smoketest-" + hex.EncodeToString(b[:])
}

func (r *Runner) keychainSmokeCheck() Check {
	return Check{
		Name: "keychain-smoke",
		Diagnose: func(_ context.Context) Result {
			secret := newSmokeSecret()

			if err := r.Keyring.Set(smokeServiceName, smokeAccountName, secret); err != nil {
				return Result{
					State:  StateError,
					Detail: "keychain Set failed: " + err.Error() + ". On Linux check that a secret-service backend (e.g. gnome-keyring, KeePassXC) is running.",
				}
			}

			got, err := r.Keyring.Get(smokeServiceName, smokeAccountName)
			if err != nil || got != secret {
				_ = r.Keyring.Delete(smokeServiceName, smokeAccountName)
				if err != nil {
					return Result{State: StateError, Detail: "keychain Get failed: " + err.Error()}
				}

				return Result{State: StateError, Detail: "keychain Get returned mismatched value (stale entry from a previous run with a failed Delete?)"}
			}

			if err := r.Keyring.Delete(smokeServiceName, smokeAccountName); err != nil {
				return Result{State: StateWarn, Detail: "keychain Delete failed: " + err.Error() + ". Set + Get worked; the test entry stays behind."}
			}

			return Result{State: StateOK, Detail: "Set/Get/Delete round-trip OK"}
		},
	}
}

// ──────────────────────────── proxy + helper ────────────────────────────

func (r *Runner) xcelerateProxyCheck() Check {
	pidPath := filepath.Join(r.XcelerateProxyDir(), "proxy.pid")

	return Check{
		Name: "xcelerate-proxy",
		Diagnose: func(_ context.Context) Result {
			content, err := os.ReadFile(pidPath) //nolint:gosec // we control the path
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return Result{State: StateWarn, Detail: "not running (no pid file). Run `bitrise-build-cache xcelerate start-proxy` after `activate` if you use the Xcode cache."}
				}

				return Result{State: StateError, Detail: "read pid file: " + err.Error()}
			}

			pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
			if err != nil {
				return Result{State: StateWarn, Detail: "pid file content invalid (" + err.Error() + ") — fixable", Fixable: true}
			}

			if err := syscall.Kill(pid, 0); err != nil {
				return Result{
					State:   StateWarn,
					Detail:  fmt.Sprintf("stale pid file: pid %d not running — fixable", pid),
					Fixable: true,
				}
			}

			return Result{State: StateOK, Detail: fmt.Sprintf("running (pid %d)", pid)}
		},
		Fix: func() (string, error) {
			if err := os.Remove(pidPath); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return "already gone: " + pidPath, nil
				}

				return "", fmt.Errorf("remove %s: %w", pidPath, err)
			}

			return "removed stale " + pidPath, nil
		},
	}
}

func (r *Runner) ccacheHelperCheck() Check {
	return Check{
		Name: "ccache-helper",
		Diagnose: func(ctx context.Context) Result {
			socketPath := r.Envs["BITRISE_CCACHE_IPC_SOCKET_PATH"]
			if socketPath == "" {
				socketPath = filepath.Join(os.TempDir(), "ccache-ipc.sock")
			}

			if _, err := os.Stat(socketPath); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return Result{State: StateWarn, Detail: "not running (no socket file). Run `bitrise-build-cache ccache start-storage-helper` if you build C/C++."}
				}

				return Result{State: StateError, Detail: "stat ccache socket: " + err.Error()}
			}

			dialer := &net.Dialer{Timeout: 500 * time.Millisecond}
			probeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			defer cancel()
			conn, err := dialer.DialContext(probeCtx, "unix", socketPath)
			if err != nil {
				return Result{
					State:   StateWarn,
					Detail:  fmt.Sprintf("socket %s present but not accepting connections (%v) — fixable", socketPath, err),
					Fixable: true,
				}
			}
			_ = conn.Close()

			return Result{State: StateOK, Detail: "running (" + socketPath + ")"}
		},
		Fix: func() (string, error) {
			socketPath := r.Envs["BITRISE_CCACHE_IPC_SOCKET_PATH"]
			if socketPath == "" {
				socketPath = filepath.Join(os.TempDir(), "ccache-ipc.sock")
			}

			if err := os.Remove(socketPath); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return "already gone: " + socketPath, nil
				}

				return "", fmt.Errorf("remove %s: %w", socketPath, err)
			}

			return "removed orphan socket " + socketPath, nil
		},
	}
}

func (r *Runner) ccacheBinaryCheck() Check {
	return Check{
		Name: "ccache-binary",
		Diagnose: func(_ context.Context) Result {
			path, err := r.LookPath("ccache")
			if err != nil {
				return Result{State: StateWarn, Detail: "ccache binary not found in PATH. Install via `brew install ccache` if you build C/C++."}
			}

			return Result{State: StateOK, Detail: "found at " + path}
		},
	}
}

// ──────────────────────────── filesystem ────────────────────────────

type logDirOutcome struct {
	Missing     string
	NotWritable string // "non-fatal" permission failure we can describe to the user
	WrongOwner  string // owned by a different uid (root, usually) — needs sudo chown
	Fatal       error
}

func checkLogDir(path string) logDirOutcome {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return logDirOutcome{Missing: path}
		}

		return logDirOutcome{Fatal: fmt.Errorf("stat %s: %w", path, err)}
	}

	if !info.IsDir() {
		return logDirOutcome{NotWritable: path + " (not a directory)"}
	}

	probe := filepath.Join(path, ".doctor-probe")
	if werr := os.WriteFile(probe, []byte{}, 0o600); werr != nil {
		// Distinguish "owned by another user" from generic write failures so the
		// hint we surface is actionable.
		if statT, ok := info.Sys().(*syscall.Stat_t); ok {
			if int(statT.Uid) != os.Geteuid() {
				return logDirOutcome{WrongOwner: path}
			}
		}

		return logDirOutcome{NotWritable: path + " (" + werr.Error() + ")"}
	}
	_ = os.Remove(probe)

	return logDirOutcome{}
}

type logDirsSummary struct {
	Missing     []string
	NotWritable []string
	WrongOwner  []string
	Fatal       error
}

func collectLogDirState(home string, candidates []string) logDirsSummary {
	var s logDirsSummary
	for _, candidate := range candidates {
		path := strings.Replace(candidate, "~", home, 1)
		out := checkLogDir(path)
		if out.Fatal != nil {
			s.Fatal = out.Fatal

			return s
		}
		if out.Missing != "" {
			s.Missing = append(s.Missing, out.Missing)
		}
		if out.NotWritable != "" {
			s.NotWritable = append(s.NotWritable, out.NotWritable)
		}
		if out.WrongOwner != "" {
			s.WrongOwner = append(s.WrongOwner, out.WrongOwner)
		}
	}

	return s
}

func resultFromLogDirsSummary(s logDirsSummary) Result {
	if s.Fatal != nil {
		return Result{State: StateError, Detail: s.Fatal.Error()}
	}
	if len(s.WrongOwner) > 0 {
		// Don't pretend doctor can fix root-owned files without elevation —
		// surface the exact command instead.
		return Result{
			State: StateError,
			Detail: fmt.Sprintf(
				"owned by another user (likely root from a previous sudo run): %s — run `sudo chown -R $(whoami) %s` to repair",
				strings.Join(s.WrongOwner, ", "),
				strings.Join(s.WrongOwner, " "),
			),
		}
	}
	if len(s.NotWritable) > 0 {
		return Result{State: StateError, Detail: "not writable: " + strings.Join(s.NotWritable, ", ")}
	}
	if len(s.Missing) > 0 {
		return Result{State: StateWarn, Detail: "missing: " + strings.Join(s.Missing, ", ") + " — fixable", Fixable: true}
	}

	return Result{State: StateOK, Detail: "all log dirs present + writable"}
}

func (r *Runner) logDirsCheck() Check {
	return Check{
		Name: "log-dirs",
		Diagnose: func(_ context.Context) Result {
			home, err := os.UserHomeDir()
			if err != nil {
				return Result{State: StateError, Detail: "could not determine home dir: " + err.Error()}
			}

			return resultFromLogDirsSummary(collectLogDirState(home, r.StateDirCandidates))
		},
		Fix: func() (string, error) {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("home dir: %w", err)
			}

			created := []string{}
			for _, candidate := range r.StateDirCandidates {
				path := strings.Replace(candidate, "~", home, 1)
				if _, err := os.Stat(path); err == nil {
					continue
				}

				if err := os.MkdirAll(path, 0o755); err != nil { //nolint:gosec
					return "", fmt.Errorf("mkdir %s: %w", path, err)
				}

				created = append(created, path)
			}

			return "created: " + strings.Join(created, ", "), nil
		},
	}
}

// ──────────────────────────── version ────────────────────────────

func (r *Runner) cliVersionCheck() Check {
	return Check{
		Name: "cli-version",
		Diagnose: func(ctx context.Context) Result {
			current := r.CLIVersion
			if current == "" {
				current = "devel"
			}

			if isLocalBuild(current) {
				return Result{State: StateOK, Detail: "current=" + current + " (local build)"}
			}

			latest, err := r.LatestReleaseTag(ctx, r.HTTPClient)
			if err != nil {
				return Result{State: StateWarn, Detail: fmt.Sprintf("current=%s; could not check latest (%v)", current, err)}
			}
			if latest == "" {
				return Result{State: StateOK, Detail: "current=" + current}
			}

			if current == latest || strings.TrimPrefix(current, "v") == strings.TrimPrefix(latest, "v") {
				return Result{State: StateOK, Detail: "current=" + current + " (up to date)"}
			}

			return Result{
				State:  StateWarn,
				Detail: fmt.Sprintf("current=%s, latest=%s — run `bitrise-build-cache update` or `brew upgrade`", current, latest),
			}
		},
	}
}

func isLocalBuild(version string) bool {
	return version == "devel" ||
		strings.Contains(version, "+") ||
		strings.Contains(version, "dirty")
}

// fetchLatestGitHubRelease returns the tag_name of the most recent CLI release.
// Network errors are returned to the caller — they render as warn.
func fetchLatestGitHubRelease(ctx context.Context, client *http.Client) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/bitrise-io/bitrise-build-cache-cli/releases/latest", nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return "", fmt.Errorf("timeout: %w", err)
		}

		return "", fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github releases API returned %s", resp.Status)
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode release payload: %w", err)
	}

	return payload.TagName, nil
}
