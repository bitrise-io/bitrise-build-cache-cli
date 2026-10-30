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

	// ActivityLogPath is the resolved `.xcactivitylog` path. When non-empty
	// the xcactivitylog collector reads it directly; otherwise it walks
	// DerivedDataPath + the Window* range to find the newest matching log.
	ActivityLogPath string

	// TargetTopN caps the per-invocation TopSteps list; defaults to 25 when 0.
	TargetTopN int
	StepTopN   int
	SwiftTopN  int
	ClangTopN  int

	// DecodeAttachments gates Phase 2 — when false the activitylog collector
	// skips the per-section attachment decode so the token walk stays strictly
	// Phase-1 only.
	DecodeAttachments bool
}

// Output is what one collector returned. Fields are nil when not produced.
type Output struct {
	Host          *host.Snapshot
	BuildTimings  *payload.BuildTimingHistogram
	BuildSettings *payload.BuildSettingsSnapshot

	// xcactivitylog collector outputs.
	Targets        []payload.TargetRow
	StepTypeHist   []payload.StepTypeBucket
	TopSteps       []payload.StepRow
	TopSwiftFuncs  []payload.SwiftTiming
	TopSwiftTypes  []payload.SwiftTiming
	LinkerStats    *payload.LinkerStatistics
	AttachmentsCCM *payload.CompilationCacheMetrics
}
