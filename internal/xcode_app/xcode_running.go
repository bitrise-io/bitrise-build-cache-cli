package xcode_app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/daemon"
)

// pgrepBin is the macOS pgrep binary.
const pgrepBin = "/usr/bin/pgrep"

// XcodeProcessName is the GUI app's main executable name. Xcode-beta is intentionally not covered.
const XcodeProcessName = "Xcode"

// XcodeProcessChecker reports whether Xcode is currently running.
type XcodeProcessChecker interface {
	RunningPIDs(ctx context.Context) ([]int, error)
}

// DefaultXcodeChecker shells out to /usr/bin/pgrep -x Xcode.
type DefaultXcodeChecker struct {
	Runner daemon.CommandRunner
}

func (d DefaultXcodeChecker) runner() daemon.CommandRunner {
	if d.Runner != nil {
		return d.Runner
	}

	return daemon.ExecRunner{}
}

// RunningPIDs returns the PIDs of running Xcode processes; empty slice = not running (pgrep exit 1).
func (d DefaultXcodeChecker) RunningPIDs(ctx context.Context) ([]int, error) {
	stdout, _, code, err := d.runner().Run(ctx, pgrepBin, "-x", XcodeProcessName)
	if err != nil {
		return nil, fmt.Errorf("pgrep -x %s: %w", XcodeProcessName, err)
	}

	if code == 1 {
		return nil, nil
	}

	return parsePIDs(stdout), nil
}

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
