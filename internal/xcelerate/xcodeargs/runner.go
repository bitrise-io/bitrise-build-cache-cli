package xcodeargs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
)

type RunStats struct {
	StartTime        time.Time
	Success          bool
	Error            error
	ExitCode         int
	DurationMS       int64
	XcodeVersion     string
	XcodeBuildNumber string
}

type DefaultRunner struct {
	logger  log.Logger
	config  xcelerate.Config
	logFile io.Writer
}

func NewRunner(logger log.Logger, config xcelerate.Config, logFile io.Writer) *DefaultRunner {
	return &DefaultRunner{
		config:  config,
		logger:  logger,
		logFile: logFile,
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
	}

	runner.logger.TInfof("Running xcodebuild command: %s", strings.Join(append([]string{xcodePath}, args...), " "))

	innerCmd := exec.CommandContext(ctx, xcodePath, args...)
	innerCmd.Stdin = os.Stdin
	var wg sync.WaitGroup
	runner.setupOutputPipes(ctx, innerCmd, &wg)

	interruptCtx, cancel := context.WithCancel(ctx)
	runner.handleInterrupt(interruptCtx, innerCmd)

	err := innerCmd.Run()
	wg.Wait()

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

func (runner *DefaultRunner) setupOutputPipes(ctx context.Context, cmd *exec.Cmd, wg *sync.WaitGroup) {
	stdOutReader, err := cmd.StdoutPipe()
	if err != nil {
		runner.logger.Errorf("Failed to get stdout pipe: %v", err)
		cmd.Stdout = os.Stdout
	} else {
		wg.Add(1)
		go func() {
			var out io.Writer
			if runner.logFile != nil {
				out = io.MultiWriter(os.Stdout, runner.logFile)
			} else {
				out = os.Stdout
			}
			runner.streamOutput(ctx, stdOutReader, out)
			wg.Done()
		}()
	}
	stdErrReader, err := cmd.StderrPipe()
	if err != nil {
		runner.logger.Errorf("Failed to get stderr pipe: %v", err)
		cmd.Stderr = os.Stderr
	} else {
		wg.Add(1)
		go func() {
			var out io.Writer
			if runner.logFile != nil {
				out = io.MultiWriter(os.Stderr, runner.logFile)
			} else {
				out = os.Stderr
			}
			runner.streamOutput(ctx, stdErrReader, out)
			wg.Done()
		}()
	}
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
	buildNumberRegexp := regexp.MustCompile(`Build version\s+(.*)`)

	output, err := exec.CommandContext(ctx, xcodebuild, "-version").Output()
	if err != nil {
		return fmt.Errorf("xcodebuild -version failed: %w", err)
	}

	runner.logger.TDebugf("xcodebuild -version output: %s", string(output))

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return fmt.Errorf("unexpected xcodebuild -version output: %s", string(output))
	}

	versionMatch := versionRegexp.FindStringSubmatch(strings.TrimSpace(lines[0]))
	runner.logger.TDebugf("xcode version match: %+v", versionMatch)
	if len(versionMatch) < 2 {
		return fmt.Errorf("failed to parse xcode version from: %s", lines[0])
	}

	runStats.XcodeVersion = versionMatch[1]

	buildNumberMatch := buildNumberRegexp.FindStringSubmatch(strings.TrimSpace(lines[1]))
	runner.logger.TDebugf("xcode build number match: %+v", buildNumberMatch)
	if len(buildNumberMatch) < 2 {
		return fmt.Errorf("failed to parse xcode build number from: %s", lines[1])
	}

	runStats.XcodeBuildNumber = buildNumberMatch[1]

	return nil
}

func (runner *DefaultRunner) streamOutput(ctx context.Context, reader io.ReadCloser, writer io.Writer) {
	scanner := bufio.NewScanner(reader)
	const maxTokenSize = 10 * 1024 * 1024
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxTokenSize)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()

		if runner.config.XcodebuildTimestamps && !runner.config.Silent {
			timestamp := time.Now().Format("15:04:05")
			line = fmt.Sprintf("[%s] %s", timestamp, line)
		}

		//nolint: errcheck
		fmt.Fprintf(writer, "%s\n", line)
	}

	// ignore error from scanner.Err()
	// when upstream reader is closed, scanner will return an error
	// which is not relevant in this context (command will close them on finish)
	//
	if err := scanner.Err(); err != nil &&
		!errors.Is(err, io.EOF) &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, os.ErrClosed) {
		runner.logger.Errorf("Failed to read from output stream: %v", err)
	}
}
