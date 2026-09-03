package ccache

import (
	"context"
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/spawn"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

type DeactivatorParams struct {
	DryRun             bool
	SocketPathOverride string
	Envs               map[string]string
	Logger             log.Logger
	OsProxy            utils.OsProxy
}

type Deactivator struct {
	logger             log.Logger
	osProxy            utils.OsProxy
	envs               map[string]string
	socketPathOverride string
	dryRun             bool
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

	osProxy := params.OsProxy
	if osProxy == nil {
		osProxy = utils.DefaultOsProxy{}
	}

	return &Deactivator{
		logger:             logger,
		osProxy:            osProxy,
		envs:               envs,
		socketPathOverride: params.SocketPathOverride,
		dryRun:             params.DryRun,
	}
}

func (d *Deactivator) Deactivate(ctx context.Context) error {
	var errs []error

	socketPath := ccacheconfig.ResolveIPCSocketPath(d.socketPathOverride, d.envs, d.osProxy)

	if d.dryRun {
		d.logger.TInfof("[dry-run] would stop ccache storage helper on %s", socketPath)
	} else {
		d.retireLegacyRegistration(ctx)

		if err := StopStorageHelperAt(ctx, d.logger, socketPath); err != nil {
			errs = append(errs, fmt.Errorf("stop ccache storage helper: %w", err))
		}
	}

	if err := ccacheconfig.Deactivate(d.logger, ccacheconfig.DeactivateParams{DryRun: d.dryRun}); err != nil {
		errs = append(errs, fmt.Errorf("deactivate C++ cache: %w", err))
	}

	return errors.Join(errs...)
}

// Ordered before the stop: a registration left by an older CLI restarts the
// helper, so deactivating would not deactivate anything.
func (d *Deactivator) retireLegacyRegistration(ctx context.Context) {
	p, err := paths.Default()
	if err != nil {
		return
	}

	if spawn.RemoveLegacySupervision(ctx, p, spawn.CcacheHelper()) {
		d.logger.TInfof("Removed a leftover service registration for the ccache storage helper")
	}
}
