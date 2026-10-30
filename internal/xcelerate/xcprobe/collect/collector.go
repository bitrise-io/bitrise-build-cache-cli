// Package collect defines the Collector contract for xcprobe sources. The
// contract has zero coupling to config: the aggregator owns the enabled-set
// decision and only instantiates collectors it was asked for.
package collect

import (
	"context"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcodebuildinfo"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/host"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/payload"
)

// Collector is one data source. Each collector runs in its own goroutine
// under PerCollectorTimeout; one failure does not kill another.
type Collector interface {
	Name() string
	Collect(ctx context.Context, in Input) (Output, error)
}

// Input is the read-only call context passed to every collector. Collectors
// are pure read: no DerivedData mutation, no file writes.
type Input struct {
	DerivedDataPath string
	Project         string
	Scheme          string
	WindowStart     time.Time
	WindowEnd       time.Time
	XcodebuildInfo  *xcodebuildinfo.Client

	// HostBase is the already-populated baseline. The host collector composes
	// it into the richer Snapshot. HostMetadata is value-only (no pointers,
	// no slices), so passing by value here cannot alias.
	HostBase common.HostMetadata

	// BenchmarkPhase is the common.BenchmarkPhase string ("baseline",
	// "warmup", or ""). Phase-3+ flag-injection collectors are hard-disabled
	// on baseline; Phase 0 collectors are unaffected.
	BenchmarkPhase string

	// XcodebuildInfoBytes is the raw tail of `xcodebuild -showBuildTimingSummary`
	// stdout for the just-run invocation, when available. nil = no-op for the
	// buildtiming collector. Populated by the wrapper's assembleArgs path; the
	// IDE path currently leaves this nil (SLF-based alternative lands in Phase 1).
	XcodebuildInfoBytes []byte
}

// Output is what one collector returned. Fields are nil when not produced.
type Output struct {
	Host          *host.Snapshot
	BuildTimings  *payload.BuildTimingHistogram
	BuildSettings *payload.BuildSettingsSnapshot
}
