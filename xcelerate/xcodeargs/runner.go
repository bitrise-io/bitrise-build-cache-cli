package xcodeargs

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/go-utils/v2/log"
)

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
func (runner *DefaultRunner) Run(ctx context.Context, args []string) error {
	xcodePath := runner.config.OriginalXcodebuildPath
	if xcodePath == "" {
		runner.logger.Warnf("no xcelerate xcode path specified, using default")
	}

	runner.logger.TInfof("Running xcodebuild command %s",
		strings.Join(append([]string{runner.config.OriginalXcodebuildPath}, args...), ","))
	innerCmd := exec.CommandContext(ctx, runner.config.OriginalXcodebuildPath, args...)
	innerCmd.Stdout = os.Stdout
	innerCmd.Stderr = os.Stderr
	innerCmd.Stdin = os.Stdin

	// Intentionally returning xcode error unwrapped

	//nolint:wrapcheck
	return innerCmd.Run()
}
