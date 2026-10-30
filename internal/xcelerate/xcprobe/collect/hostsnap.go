package collect

import (
	"context"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/host"
)

// HostSnapCollector wraps host.Capture.
type HostSnapCollector struct{}

func (HostSnapCollector) Name() string { return "hostsnap" }

func (HostSnapCollector) Collect(ctx context.Context, in Input) (Output, error) {
	snap := host.Capture(ctx, in.HostBase)

	return Output{Host: &snap}, nil
}
