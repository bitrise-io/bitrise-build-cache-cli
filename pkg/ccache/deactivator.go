package ccache

import (
	"context"
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// DeactivatorParams configures the ccache deactivation.
type DeactivatorParams struct {
	// DryRun logs intended removals without executing them.
	DryRun bool

	// SocketPathOverride is the IPC socket path the storage helper is expected
	// to listen on. Empty falls back to ccacheconfig.ResolveIPCSocketPath.
	SocketPathOverride string

	// Envs is the env var source consulted for the socket-path env override.
	// Nil means utils.AllEnvs().
	Envs map[string]string

	// Logger overrides the default logger. If nil, a default logger is created.
	Logger log.Logger

	// OsProxy overrides the default OS proxy. If nil, utils.DefaultOsProxy{} is used.
	OsProxy utils.OsProxy
}

// Deactivator stops the ccache storage helper and removes the ccache config.
type Deactivator struct {
	logger             log.Logger
	osProxy            utils.OsProxy
	envs               map[string]string
	socketPathOverride string
	dryRun             bool
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

// Deactivate stops the running storage helper (best-effort) and drops the
// ccache config artefact.
func (d *Deactivator) Deactivate(ctx context.Context) error {
	var errs []error

	socketPath := ccacheconfig.ResolveIPCSocketPath(d.socketPathOverride, d.envs, d.osProxy)

	if d.dryRun {
		d.logger.TInfof("[dry-run] would stop ccache storage helper on %s", socketPath)
	} else if err := StopStorageHelperAt(ctx, d.logger, socketPath); err != nil {
		errs = append(errs, fmt.Errorf("stop ccache storage helper: %w", err))
	}

	if err := ccacheconfig.Deactivate(d.logger, ccacheconfig.DeactivateParams{DryRun: d.dryRun}); err != nil {
		errs = append(errs, fmt.Errorf("deactivate C++ cache: %w", err))
	}

	return errors.Join(errs...)
}
