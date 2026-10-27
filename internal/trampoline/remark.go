package trampoline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildidentity"
)

const remarkText = "remark: Bitrise remote build cache engaged"

// visitURLTemplate mirrors enrichment.VisitURL. Inlined to keep the trampoline
// binary free of the enrichment package's heavy transitive dependencies
// (auth, blobstats, analytics). Must stay in sync with
// internal/xcelerate/enrichment/enricher.go:VisitURL.
const visitURLTemplate = "https://app.bitrise.io/build-cache/invocations/xcode/%s"

const startTimeLookupTimeout = 200 * time.Millisecond

// remarkDeps lets tests swap pid lookup, start-time lookup, sentinel creation, and stderr.
type remarkDeps struct {
	readProxyPid   func() (int, bool)
	proxyStartTime func(pid int) (int64, bool)
	createSentinel func(path string) (ok bool, err error)
	stderr         io.Writer
	tmpDir         func() string
	// ancestry resolves the shim's own ancestry chain for predicted-InvocationID
	// derivation. Returns an empty slice when unresolvable (zero PID, lookup
	// timeout) — callers suppress Visit URL emission in that case.
	ancestry func() []buildidentity.AncestryEntry
}

//nolint:gochecknoglobals // production dependency bundle; tests swap atoms.
var defaultRemarkDeps = remarkDeps{
	readProxyPid: func() (int, bool) {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, false
		}

		return readPidFile(filepath.Join(home, paths.XcelerateRootRelative, paths.ProxyPidFileName))
	},
	proxyStartTime: lookupProcessStartTimeMS,
	createSentinel: func(path string) (bool, error) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644) //nolint:gosec // tmp-dir sentinel
		if err != nil {
			if errors.Is(err, fs.ErrExist) {
				return false, nil
			}

			return false, err //nolint:wrapcheck
		}
		_ = f.Close()

		return true, nil
	},
	stderr:   os.Stderr,
	tmpDir:   os.TempDir,
	ancestry: func() []buildidentity.AncestryEntry { return buildidentity.WalkAncestryForPID(os.Getpid()) },
}

// EmitEngagedRemark prints the Xcode-surfaced engagement remark exactly once
// per proxy session. Shim must only call it AFTER a successful proxy socket
// probe — a missing pid file means no proxy is up yet and the remark is skipped.
//
// When the shim's ancestry resolves to an xcodebuild / Xcode / SWBBuildService
// anchor, also prints the invocation Visit URL (deduped by predicted
// InvocationID so each anchor gets exactly one URL).
func EmitEngagedRemark() {
	emitEngagedRemark(defaultRemarkDeps)
}

func emitEngagedRemark(d remarkDeps) {
	pid, ok := d.readProxyPid()
	if !ok {
		return
	}

	emitEngagedLine(d, pid)
	emitVisitURL(d)
}

func emitEngagedLine(d remarkDeps, proxyPID int) {
	sentinel := filepath.Join(d.tmpDir(), engagedSentinelName(proxyPID, d.proxyStartTime))
	created, err := d.createSentinel(sentinel)
	if err != nil || !created {
		return
	}

	fmt.Fprintln(d.stderr, remarkText)
}

func emitVisitURL(d remarkDeps) {
	id, ok := derivePredictedInvocationID(d)
	if !ok {
		return
	}

	sentinel := filepath.Join(d.tmpDir(), paths.TrampolineVisitURLSentinelPrefix+id)
	created, err := d.createSentinel(sentinel)
	if err != nil || !created {
		return
	}

	fmt.Fprintf(d.stderr, "remark: Invocation saved. Visit 👉 "+visitURLTemplate+"\n", id)
}

// derivePredictedInvocationID resolves the shim's ancestry to the first
// anchorable process (xcodebuild / Xcode / SWBBuildService) and returns the
// deterministic InvocationID derived from it. Returns ("", false) when no
// anchor is present (SPM-only, bazel, standalone swiftc) — Visit URL is
// suppressed in that case.
func derivePredictedInvocationID(d remarkDeps) (string, bool) {
	if d.ancestry == nil {
		return "", false
	}

	chain := d.ancestry()
	if len(chain) == 0 {
		return "", false
	}

	anchor, ok := buildidentity.AnchorFromAncestry(chain)
	if !ok {
		return "", false
	}

	return buildidentity.Derive(anchor), true
}

// engagedSentinelName encodes pid plus proxy start-time-ms so a recycled PID
// across a proxy restart doesn't collide with a stale sentinel. Falls back to
// pid-only when start-time is unresolvable (accepts the stale-pid edge case;
// /tmp is swept on reboot and a lingering sentinel just silences one remark).
func engagedSentinelName(pid int, lookup func(int) (int64, bool)) string {
	if lookup != nil {
		if start, ok := lookup(pid); ok {
			return paths.TrampolineRemarkSentinelPrefix + strconv.Itoa(pid) + "-" + strconv.FormatInt(start, 10)
		}
	}

	return paths.TrampolineRemarkSentinelPrefix + strconv.Itoa(pid)
}

func lookupProcessStartTimeMS(pid int) (int64, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), startTimeLookupTimeout)
	defer cancel()

	proc, err := process.NewProcessWithContext(ctx, int32(pid)) //nolint:gosec // PIDs fit in int32
	if err != nil {
		return 0, false
	}

	ct, err := proc.CreateTimeWithContext(ctx)
	if err != nil {
		return 0, false
	}

	return ct, true
}

func readPidFile(path string) (int, bool) {
	raw, err := os.ReadFile(path) //nolint:gosec // caller-controlled path under user's own home
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))

	return pid, err == nil && pid > 0
}
