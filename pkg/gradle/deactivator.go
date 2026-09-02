// Package gradle is the public API for the Gradle deactivate flow. The
// Activator lives under pkg/gradle/mirrors — this package will grow additional
// entry points as more Gradle-side commands migrate down from cmd/.
package gradle

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// DeactivatorParams configures the Gradle deactivation.
type DeactivatorParams struct {
	// DryRun logs intended removals without executing them.
	DryRun bool

	// Envs is the env var source used to resolve GRADLE_USER_HOME. Nil means
	// utils.AllEnvs().
	Envs map[string]string

	// Logger overrides the default logger. If nil, a default logger is created.
	Logger log.Logger
}

// Deactivator removes the Gradle activation artefacts (init script,
// gradle.properties block, sidecar).
type Deactivator struct {
	logger log.Logger
	envs   map[string]string
	dryRun bool
}

// NewDeactivator creates a Deactivator with production defaults.
func NewDeactivator(params DeactivatorParams) *Deactivator {
	logger := params.Logger
	if logger == nil {
		logger = log.NewLogger()
	}

	envs := params.Envs
	if envs == nil {
		envs = utils.AllEnvs()
	}

	return &Deactivator{
		logger: logger,
		envs:   envs,
		dryRun: params.DryRun,
	}
}

// Deactivate removes the Gradle activation artefacts.
func (d *Deactivator) Deactivate(_ context.Context) error {
	p, err := paths.Default()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}

	gradleHome := p.GradleHome(d.envs[paths.GradleUserHomeEnvKey])

	if err := gradleconfig.Deactivate(d.logger, gradleconfig.DeactivateParams{
		GradleHome: gradleHome,
		Home:       p.Home,
		DryRun:     d.dryRun,
	}); err != nil {
		return fmt.Errorf("deactivate Gradle: %w", err)
	}

	return nil
}
