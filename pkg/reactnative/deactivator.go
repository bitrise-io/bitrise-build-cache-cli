package reactnative

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	rnconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/reactnative"
)

// DeactivatorParams configures the React Native deactivation.
type DeactivatorParams struct {
	// DryRun logs intended removals without executing them.
	DryRun bool

	// Logger overrides the default logger. If nil, a default logger is created.
	Logger log.Logger
}

// Deactivator removes the React Native marker file. Fan-out to gradle, xcode
// and ccache stays at the cmd/ layer so per-tool errors remain granular.
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

// Deactivate removes the React Native marker.
func (d *Deactivator) Deactivate(_ context.Context) error {
	if err := rnconfig.Deactivate(d.logger, rnconfig.DeactivateParams{DryRun: d.dryRun}); err != nil {
		return fmt.Errorf("deactivate react-native marker: %w", err)
	}

	return nil
}
