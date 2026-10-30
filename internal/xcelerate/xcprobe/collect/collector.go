// Package collect defines the Collector contract for xcprobe sources.
package collect

import (
	"context"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodebuildinfo"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/host"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/payload"
)

// Collector is one data source. Each runs in its own goroutine; one failure
// does not kill another.
type Collector interface {
	Name() string
	Collect(ctx context.Context, in Input) (Output, error)
}

// Input is the read-only call context passed to every collector. Collectors
// must not mutate DerivedData or write any files.
type Input struct {
	DerivedDataPath string
	Project         string
	Scheme          string
	WindowStart     time.Time
	WindowEnd       time.Time
	XcodebuildInfo  *xcodebuildinfo.Client

	// HostBase is composed into the richer host.Snapshot by the host collector.
	HostBase common.HostMetadata

	BenchmarkPhase string

	// XcodebuildInfoBytes is the raw stdout tail of
	// `xcodebuild -showBuildTimingSummary` for the just-run invocation; nil
	// no-ops the buildtiming collector. Populated by the wrapper path only.
	XcodebuildInfoBytes []byte
}

// Output is what one collector returned. Fields are nil when not produced.
type Output struct {
	Host          *host.Snapshot
	BuildTimings  *payload.BuildTimingHistogram
	BuildSettings *payload.BuildSettingsSnapshot
}
