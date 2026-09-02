// Package bazel is the public API for the Bazel deactivate flow.
package bazel

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/bazel"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// DeactivatorParams configures the Bazel deactivation.
type DeactivatorParams struct {
	// DryRun logs intended removals without executing them.
	DryRun bool

	// Logger overrides the default logger. If nil, a default logger is created.
	Logger log.Logger
}

// Deactivator removes the Bazel activation artefacts (bazelrc block, sidecar).
type Deactivator struct {
	logger log.Logger
	dryRun bool
}

// NewDeactivator creates a Deactivator with production defaults.
func NewDeactivator(params DeactivatorParams) *Deactivator {
	logger := params.Logger
	if logger == nil {
		logger = log.NewLogger()
	}

	return &Deactivator{
		logger: logger,
		dryRun: params.DryRun,
	}
}

// Deactivate removes the Bazel activation artefacts.
func (d *Deactivator) Deactivate(_ context.Context) error {
	p, err := paths.Default()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}

	if err := bazelconfig.Deactivate(d.logger, bazelconfig.DeactivateParams{
		BazelrcPath: p.BazelrcFile(),
		Home:        p.Home,
		DryRun:      d.dryRun,
	}); err != nil {
		return fmt.Errorf("deactivate Bazel: %w", err)
	}

	return nil
}
