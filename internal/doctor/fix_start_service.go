package doctor

import (
	"context"
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/spawn"
)

// StartServiceFixer starts a cache service the way a build does.
type StartServiceFixer struct {
	Label   string
	Service spawn.Service

	// Start is a test seam; nil uses the real spawn.
	Start func(spawn.Service) (int, error)
}

func StartProxyFixer() StartServiceFixer {
	return StartServiceFixer{Label: "xcelerate proxy", Service: spawn.XcelerateProxy()}
}

func StartCcacheHelperFixer() StartServiceFixer {
	return StartServiceFixer{Label: "ccache storage helper", Service: spawn.CcacheHelper()}
}

func (f StartServiceFixer) Fix() (string, error) {
	ctx := context.Background()

	if p, err := paths.Default(); err == nil {
		_ = spawn.RemoveLegacySupervision(ctx, p, f.Service)
	}

	start := f.Start
	if start == nil {
		start = spawn.Detached
	}

	pid, err := start(f.Service)
	if err != nil {
		return "", fmt.Errorf("start the %s: %w", f.Label, err)
	}

	return fmt.Sprintf("started the %s (pid %d); a build would have started it too", f.Label, pid), nil
}
