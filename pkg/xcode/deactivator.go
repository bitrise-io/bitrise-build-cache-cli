// Package xcode is the public API for Xcode-related build cache commands. The
// deactivate flow lives here directly; the activate flow still lives inside
// internal/config/xcelerate and is called from cmd/xcode.
package xcode

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// DeactivatorParams configures the Xcode deactivation.
type DeactivatorParams struct {
	// DryRun logs intended removals without executing them.
	DryRun bool

	// Envs is the env var source consulted when resolving $HOME and reading the
	// shell RC files. Nil means utils.AllEnvs().
	Envs map[string]string

	// Logger overrides the default logger. If nil, a default logger is created.
	Logger log.Logger
}

// Deactivator stops the xcelerate proxy, removes ~/.bitrise-xcelerate/, and
// strips the "Bitrise Xcelerate" export block from the shell RC files.
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

// Deactivate removes the Xcode activation artefacts and stops the proxy.
func (d *Deactivator) Deactivate(_ context.Context) error {
	if err := xcelerate.Deactivate(d.logger, xcelerate.DeactivateParams{
		Envs:   d.envs,
		DryRun: d.dryRun,
	}); err != nil {
		return fmt.Errorf("deactivate Xcode: %w", err)
	}

	return nil
}
