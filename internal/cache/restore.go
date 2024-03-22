package cache

import (
	"github.com/bitrise-io/go-steputils/v2/cache"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
)

const (
	restoreStepId = "restore-gradle-build-cache-diagnostics"
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
	return cache.NewRestorer(step.envRepo, step.logger, step.commandFactory).Restore(cache.RestoreCacheInput{
		StepId:         restoreStepId,
		Verbose:        isVerboseMode,
		Keys:           []string{key},
		NumFullRetries: 3,
	})
}
