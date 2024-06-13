package diagnostics

import (
	"fmt"
	"os"

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

func (step GradleOutputDiagnosticsRestorer) Run(isVerboseMode bool) (bool, error) {
	if err := cache.NewRestorer(step.envRepo, step.logger, step.commandFactory, nil).Restore(cache.RestoreCacheInput{
		StepId:         restoreStepID,
		Verbose:        isVerboseMode,
		Keys:           []string{key},
		NumFullRetries: numRestoreRetries,
	}); err != nil {
		return false, fmt.Errorf("failed to restore cache: %w", err)
	}

	foundRestoredData, err := folderExistsNotEmpty(".gradle")
	if err != nil {
		return false, fmt.Errorf("failed to check if Gradle output data was restored: %w", err)
	}

	return foundRestoredData, nil
}

func folderExistsNotEmpty(folderPath string) (bool, error) {
	_, err := os.Stat(folderPath)
	if os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("failed to check if folder exists: %w", err)
	}

	entries, err := os.ReadDir(folderPath)
	if err != nil {
		return false, fmt.Errorf("failed to read folder entries: %w", err)
	}

	if len(entries) > 0 {
		return true, nil
	}

	return false, nil
}
