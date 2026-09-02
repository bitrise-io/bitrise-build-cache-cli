package gradle

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

type DeactivatorParams struct {
	DryRun bool
	Envs   map[string]string
	Logger log.Logger
}

type Deactivator struct {
	logger log.Logger
	envs   map[string]string
	dryRun bool
}

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
