package xcode

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
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

func (d *Deactivator) Deactivate(ctx context.Context) error {
	if err := xcelerate.Deactivate(ctx, d.logger, xcelerate.DeactivateParams{
		Envs:   d.envs,
		DryRun: d.dryRun,
	}); err != nil {
		return fmt.Errorf("deactivate Xcode: %w", err)
	}

	return nil
}
