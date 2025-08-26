package xcodeargs

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/go-utils/v2/log"
)

type RunStats struct {
	StartTime  time.Time
	Success    bool
	Error      error
	DurationMS int64
}

type DefaultRunner struct {
	logger log.Logger
	config xcelerate.Config
}

func NewRunner(logger log.Logger, config xcelerate.Config) *DefaultRunner {
	return &DefaultRunner{
		config: config,
		logger: logger,
	}
}

// nolint: gosec
func (runner *DefaultRunner) Run(ctx context.Context, args []string) RunStats {
	xcodePath := runner.config.OriginalXcodebuildPath
	if xcodePath == "" {
		runner.logger.Warnf("no xcelerate xcode path specified, using default")
		xcodePath = xcelerate.DefaultXcodePath
	}

	runner.logger.TInfof("Running xcodebuild command: %s", strings.Join(append([]string{xcodePath}, args...), " "))

	startTime := time.Now()

	innerCmd := exec.CommandContext(ctx, xcodePath, args...)
	innerCmd.Stdout = os.Stdout
	innerCmd.Stderr = os.Stderr
	innerCmd.Stdin = os.Stdin

	err := innerCmd.Run()

	duration := time.Since(startTime)

	runstats := RunStats{
		StartTime:  startTime,
		Success:    err == nil,
		Error:      err,
		DurationMS: duration.Milliseconds(),
	}

	return runstats
}
