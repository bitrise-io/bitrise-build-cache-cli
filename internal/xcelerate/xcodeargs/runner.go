package xcodeargs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
)

type RunStats struct {
	StartTime         time.Time
	Success           bool
	Error             error
	ExitCode          int
	DurationMS        int64
	XcodeVersion      string
	XcodeBuildVersion string
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

	runStats := RunStats{
		StartTime: time.Now(),
	}

	if err := runner.determineXcodeVersionAndBuildNumber(ctx, xcodePath, &runStats); err != nil {
		runner.logger.TErrorf("Failed to determine xcode version and build number: %+v", err)

		runStats.Error = err
		runStats.Success = false

		return runStats
	}

	runner.logger.TInfof("Running xcodebuild command: %s", strings.Join(append([]string{xcodePath}, args...), " "))

	innerCmd := exec.CommandContext(ctx, xcodePath, args...)
	innerCmd.Stderr = os.Stderr
	innerCmd.Stdin = os.Stdin

	interruptCtx, cancel := context.WithCancel(ctx)
	runner.handleInterrupt(interruptCtx, innerCmd)

	err := innerCmd.Run()

	cancel()
	duration := time.Since(runStats.StartTime)

	runStats.DurationMS = duration.Milliseconds()
	if err != nil {
		runStats.Error = err
		runStats.Success = false

		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			runStats.ExitCode = exitError.ExitCode()
		} else {
			runStats.ExitCode = 1
		}
	} else {
		runStats.Success = true
	}

	return runStats
}

func (runner *DefaultRunner) handleInterrupt(ctx context.Context, cmd *exec.Cmd) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		code := <-sig

		// Shutdown signal with grace period of 15 seconds
		shutdownCtx, _ := context.WithTimeout(ctx, 15*time.Second) //nolint: govet

		go func() {
			select {
			case <-ctx.Done():
				return
			case <-shutdownCtx.Done():
			}

			if errors.Is(shutdownCtx.Err(), context.DeadlineExceeded) {
				runner.logger.Errorf("Graceful shutdown timed out... forcing exit.")
				killErr := cmd.Process.Kill()
				if killErr != nil {
					runner.logger.Errorf("Failed to kill process: %+v", killErr)
				}
			}
		}()

		runner.logger.TInfof("Signalling xcodebuild to terminate with code: %s", code.String())
		err := cmd.Process.Signal(code)
		if err != nil {
			runner.logger.Errorf("Graceful shutdown failed: %+v", err)
		}
	}()
}

func (runner *DefaultRunner) determineXcodeVersionAndBuildNumber(ctx context.Context, xcodebuild string, runStats *RunStats) error {
	runner.logger.TDebugf("Checking xcodebuild version and build number: %s -version", xcodebuild)

	versionRegexp := regexp.MustCompile(`Xcode\s+(.*)`)
	buildVersionRegexp := regexp.MustCompile(`Build version\s+(.*)`)

	output, err := exec.CommandContext(ctx, xcodebuild, "-version").Output()
	if err != nil {
		return fmt.Errorf("xcodebuild -version failed: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return fmt.Errorf("unexpected xcodebuild -version output: %s", string(output))
	}

	versionMatch := versionRegexp.FindStringSubmatch(strings.TrimSpace(lines[0]))
	if len(versionMatch) < 2 {
		return fmt.Errorf("failed to parse xcode version from: %s", lines[0])
	}

	runStats.XcodeVersion = versionMatch[1]

	buildVersionMatch := buildVersionRegexp.FindStringSubmatch(strings.TrimSpace(lines[1]))
	if len(buildVersionMatch) < 2 {
		return fmt.Errorf("failed to parse xcode build number from: %s", lines[1])
	}

	runStats.XcodeBuildVersion = buildVersionMatch[1]

	return nil
}
