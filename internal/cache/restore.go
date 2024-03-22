package cache

import (
	"fmt"

	"github.com/bitrise-io/go-steputils/v2/cache"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
)

const (
	numRestoreRetries = 3
	restoreStepID     = "restore-gradle-build-cache-diagnostics"
)

type GradleOutputDiagnosticsRestorer struct {
	logger         log.Logger
	commandFactory command.Factory
	envRepo        env.Repository
}

func NewGradleDiagnosticOuptutRestorer(
	logger log.Logger,
	commandFactory command.Factory,
	envRepo env.Repository,
) GradleOutputDiagnosticsRestorer {
	return GradleOutputDiagnosticsRestorer{
		logger:         logger,
		commandFactory: commandFactory,
		envRepo:        envRepo,
	}
}

func (step GradleOutputDiagnosticsRestorer) Run(isVerboseMode bool) error {
	if err := cache.NewRestorer(step.envRepo, step.logger, step.commandFactory).Restore(cache.RestoreCacheInput{
		StepId:         restoreStepID,
		Verbose:        isVerboseMode,
		Keys:           []string{key},
		NumFullRetries: numRestoreRetries,
	}); err != nil {
		return fmt.Errorf("failed to restore cache: %w", err)
	}

	return nil
}
