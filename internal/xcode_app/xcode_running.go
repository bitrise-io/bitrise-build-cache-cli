package xcode_app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/daemon"
)

// pgrepBin is the macOS pgrep binary. Tests inject a stub via the Runner
// field on DefaultXcodeChecker rather than mutating this constant.
const pgrepBin = "/usr/bin/pgrep"

// XcodeProcessName is the executable name `pgrep -x` matches against. The
// GUI app's main process is literally `Xcode`; `Xcode-beta` would have a
// different process name and isn't covered here intentionally — beta users
// can re-run enable themselves.
const XcodeProcessName = "Xcode"

// XcodeProcessChecker reports whether Xcode is currently running. It is a
// no-arg function on purpose so the activator only needs to inject a
// concrete checker (real or fake) without threading context through extra
// indirection.
type XcodeProcessChecker interface {
	RunningPIDs(ctx context.Context) ([]int, error)
}

// DefaultXcodeChecker shells out to /usr/bin/pgrep -x Xcode. Reuses the
// daemon package's CommandRunner contract so tests can drive deterministic
// pgrep replies without forking real processes.
type DefaultXcodeChecker struct {
	Runner daemon.CommandRunner
}

func (d DefaultXcodeChecker) runner() daemon.CommandRunner {
	if d.Runner != nil {
		return d.Runner
	}

	return daemon.ExecRunner{}
}

// RunningPIDs returns the PIDs of running Xcode processes. pgrep exits 1
// with no output when no process matches — we treat that as "not running"
// (empty slice, no error), matching the spec'd pgrep contract.
func (d DefaultXcodeChecker) RunningPIDs(ctx context.Context) ([]int, error) {
	stdout, _, code, err := d.runner().Run(ctx, pgrepBin, "-x", XcodeProcessName)
	if err != nil {
		// "pgrep not found" or another exec-side error. Surface as "we
		// couldn't tell"; the caller's policy decides whether to abort
		// enable or just skip the relaunch nudge.
		return nil, fmt.Errorf("pgrep -x %s: %w", XcodeProcessName, err)
	}

	// pgrep exit code 1 == no match. Anything else above 1 == operational
	// error (bad arg, etc.); we don't try to differentiate further because
	// the only thing we'd do is log it.
	if code == 1 {
		return nil, nil
	}

	return parsePIDs(stdout), nil
}

// parsePIDs splits pgrep's stdout into a slice of PIDs, dropping anything
// that isn't a positive integer. Defensive against trailing newlines.
func parsePIDs(stdout string) []int {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	pids := make([]int, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		n, err := strconv.Atoi(trimmed)
		if err != nil || n <= 0 {
			continue
		}

		pids = append(pids, n)
	}

	return pids
}
