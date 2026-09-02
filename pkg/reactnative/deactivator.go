package reactnative

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	rnconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/reactnative"
)

type DeactivatorParams struct {
	DryRun bool
	Logger log.Logger
}

// Deactivator removes only the React Native marker; the cmd/ layer fans out to
// gradle, xcode and ccache so per-tool errors stay separate.
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
	if err := rnconfig.Deactivate(d.logger, rnconfig.DeactivateParams{DryRun: d.dryRun}); err != nil {
		return fmt.Errorf("deactivate react-native marker: %w", err)
	}

	return nil
}
