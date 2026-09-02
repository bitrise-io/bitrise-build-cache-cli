package bazel

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/bazel"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

type DeactivatorParams struct {
	DryRun bool
	Logger log.Logger
}

type Deactivator struct {
	logger log.Logger
	dryRun bool
}

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
