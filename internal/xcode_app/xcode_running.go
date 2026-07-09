package xcode_app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/daemon"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/exec"
)

const pgrepBin = "/usr/bin/pgrep"

// XcodeProcessName intentionally omits Xcode-beta.
const XcodeProcessName = "Xcode"

type XcodeProcessChecker interface {
	RunningPIDs(ctx context.Context) ([]int, error)
}

type DefaultXcodeChecker struct {
	Runner daemon.CommandRunner
}

func (d DefaultXcodeChecker) runner() daemon.CommandRunner {
	if d.Runner != nil {
		return d.Runner
	}

	return exec.ExecRunner{}
}

// RunningPIDs: pgrep exit 1 = no match; exit >1 (2 syntax, 3 fatal) = error.
func (d DefaultXcodeChecker) RunningPIDs(ctx context.Context) ([]int, error) {
	stdout, stderr, code, err := d.runner().Run(ctx, pgrepBin, "-x", XcodeProcessName)
	if err != nil {
		return nil, fmt.Errorf("pgrep -x %s: %w", XcodeProcessName, err)
	}

	switch code {
	case 0:
		return parsePIDs(stdout), nil
	case 1:
		return nil, nil
	default:
		return nil, fmt.Errorf("pgrep -x %s exited %d: %s", XcodeProcessName, code, strings.TrimSpace(stderr))
	}
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
