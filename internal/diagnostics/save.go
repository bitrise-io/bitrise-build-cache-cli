package diagnostics

import (
	"fmt"
	"strings"

	"github.com/bitrise-io/go-steputils/v2/cache"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
)

const (
	saveStepID = "save-gradle-build-cache-diagnostics"

	// Cache key template
	// OS + Arch: to guarantee that stack-specific content (absolute paths, binaries) are stored separately
	// Workflow: current Bitrise workflow name (for example, primary).
	key = `{{ .OS }}-{{ .Arch }}-gradle-build-cache-diagnostics-{{ .Workflow }}`
)

// Cached paths
var paths = []string{ //nolint:gochecknoglobals
	"**/build",
	".gradle",
}

type GradleDiagnosticOutputSaver struct {
	logger       log.Logger
	pathChecker  pathutil.PathChecker
	pathProvider pathutil.PathProvider
	pathModifier pathutil.PathModifier
	envRepo      env.Repository
}

func NewGradleDiagnosticOuptutSaver(
	logger log.Logger,
	pathChecker pathutil.PathChecker,
	pathProvider pathutil.PathProvider,
	pathModifier pathutil.PathModifier,
	envRepo env.Repository,
) GradleDiagnosticOutputSaver {
	return GradleDiagnosticOutputSaver{
		logger:       logger,
		pathChecker:  pathChecker,
		pathProvider: pathProvider,
		pathModifier: pathModifier,
		envRepo:      envRepo,
	}
}

func (s GradleDiagnosticOutputSaver) Run(isVerboseMode bool) error {
	s.logger.Debugf("Cache key: %s", key)
	s.logger.Debugf("Cache paths:")
	s.logger.Debugf(strings.Join(paths, "\n"))

	saver := cache.NewSaver(s.envRepo, s.logger, s.pathProvider, s.pathModifier, s.pathChecker, nil)

	if err := saver.Save(cache.SaveCacheInput{
		StepId:      saveStepID,
		Verbose:     isVerboseMode,
		Key:         key,
		Paths:       paths,
		IsKeyUnique: true,
	}); err != nil {
		return fmt.Errorf("failed to save cache: %w", err)
	}

	return nil
}
