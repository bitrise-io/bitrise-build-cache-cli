package xcodeargs

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
)

type RunStats struct {
	StartTime    time.Time
	Success      bool
	Error        error
	DurationMS   int64
	XcodeVersion string
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

	runStats := RunStats{
		StartTime: time.Now(),
	}

	innerCmd := exec.CommandContext(ctx, xcodePath, args...)

	wg := sync.WaitGroup{}
	stdOutReader, err := innerCmd.StdoutPipe()
	if err != nil {
		runner.logger.Errorf("Failed to get stdout pipe: %v", err)
		innerCmd.Stdout = os.Stdout
	} else {
		wg.Add(1)
		go func() {
			runner.streamAndMatchStdOut(ctx, stdOutReader, &runStats)
			wg.Done()
		}()
	}

	innerCmd.Stderr = os.Stderr
	innerCmd.Stdin = os.Stdin

	err = innerCmd.Run()

	wg.Wait()
	duration := time.Since(runStats.StartTime)

	runStats.DurationMS = duration.Milliseconds()
	if err != nil {
		runStats.Error = err
		runStats.Success = false
	} else {
		runStats.Success = true
	}

	return runStats
}

func (runner *DefaultRunner) streamAndMatchStdOut(ctx context.Context, reader io.ReadCloser, runStats *RunStats) {
	versionRegex := regexp.MustCompile(`/Applications/Xcode[-_]?([\w.]+).app/Contents`)

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()

		if runStats.XcodeVersion == "" {
			matches := versionRegex.FindStringSubmatch(line)
			if len(matches) > 1 {
				runStats.XcodeVersion = matches[1]
				runner.logger.TInfof("Detected Xcode version: %s", runStats.XcodeVersion)
			}
		}

		//nolint: errcheck
		fmt.Fprintf(os.Stdout, "%s\n", line)
	}

	if err := scanner.Err(); err != nil {
		runner.logger.Errorf("Failed to scan stdout: %v", err)
	}
}
