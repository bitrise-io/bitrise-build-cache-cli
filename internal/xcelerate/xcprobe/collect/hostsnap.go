package collect

import (
	"context"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/host"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/payload"
)

// HostSnapCollector wraps host.Capture.
type HostSnapCollector struct{}

func (HostSnapCollector) Name() string { return "hostsnap" }

func (HostSnapCollector) Collect(ctx context.Context, in Input) (Output, error) {
	snap := host.Capture(ctx, in.HostBase)

	return Output{Host: hostSnapshotToPayload(snap)}, nil
}

func hostSnapshotToPayload(s host.Snapshot) *payload.HostSnapshot {
	return &payload.HostSnapshot{
		OS:                s.OS,
		CPUCores:          s.CPUCores,
		MemSize:           s.MemSize,
		Locale:            s.Locale,
		DefaultCharset:    s.DefaultCharset,
		Hostname:          s.Hostname,
		Username:          s.Username,
		ThermalPressure:   s.ThermalPressure,
		MemFreeMB:         s.MemFreeMB,
		SwapTotalMB:       s.SwapTotalMB,
		SwapFreeMB:        s.SwapFreeMB,
		UptimeSeconds:     s.UptimeSeconds,
		IsVirtual:         s.IsVirtual,
		SpotlightActive:   s.SpotlightActive,
		TimeMachineActive: s.TimeMachineActive,
		ModelID:           s.ModelID,
		Chip:              s.Chip,
	}
}
