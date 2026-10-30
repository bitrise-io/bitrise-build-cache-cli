// Package payload is the xcprobe enrichment payload type and the one-way
// edge onto analytics.Invocation (via json.RawMessage, so analytics cannot
// import back).
package payload

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/host"
)

// EnrichmentPayloadSchemaVersion is bumped only when a field's meaning
// changes; purely additive fields are safe because the BE tolerates unknown
// JSON keys. Rev 2 adds xcactivitylog-collector surfaces (targets, step-type
// histogram, top steps, swift timings, linker statistics) and the Phase 2
// aggregated CompilationCacheMetrics.
const EnrichmentPayloadSchemaVersion = 2

// EnrichmentPayload is the top-level envelope attached to Invocation.Enrichment.
type EnrichmentPayload struct {
	SchemaVersion   int               `json:"schemaVersion"`
	CollectorErrors map[string]string `json:"collectorErrors,omitempty"`

	Host          *host.Snapshot         `json:"host,omitempty"`
	BuildSettings *BuildSettingsSnapshot `json:"buildSettings,omitempty"`
	BuildTimings  *BuildTimingHistogram  `json:"buildTimings,omitempty"`

	// xcactivitylog collector surfaces (Phase 1).
	Targets       []TargetRow       `json:"targets,omitempty"`
	StepTypeHist  []StepTypeBucket  `json:"stepTypeHist,omitempty"`
	TopSteps      []StepRow         `json:"topSteps,omitempty"`
	TopSwiftFuncs []SwiftTiming     `json:"topSwiftFuncs,omitempty"`
	TopSwiftTypes []SwiftTiming     `json:"topSwiftTypes,omitempty"`
	LinkerStats   *LinkerStatistics `json:"linkerStats,omitempty"`

	// Phase 2 — aggregated CompilationCacheMetrics across all attachment
	// bearing sections.
	AttachmentsCCM *CompilationCacheMetrics `json:"attachmentsCCM,omitempty"`
}

// TargetRow is one XCMetrics-style per-target row derived from a top-level
// IDEActivityLogSection whose domain starts with
// `Xcode.IDEActivityLogDomainType.target`. The cache-fetch bit mirrors
// `wasFetchedFromCache` on the underlying section.
type TargetRow struct {
	Name                  string `json:"name"`
	DurationMs            int64  `json:"durationMs"`
	CompilationDurationMs int64  `json:"compilationDurationMs,omitempty"`
	FetchedFromCache      bool   `json:"fetchedFromCache,omitempty"`
	Warnings              int    `json:"warnings,omitempty"`
	Errors                int    `json:"errors,omitempty"`
	StepCount             int    `json:"stepCount,omitempty"`
}

// StepTypeBucket is one row of the per-DetailStepType histogram.
type StepTypeBucket struct {
	Type    string `json:"type"`
	Count   int    `json:"count"`
	TotalMs int64  `json:"totalMs"`
}

// StepRow is one entry on the Top-N longest-steps list.
type StepRow struct {
	Type       string `json:"type"`
	Title      string `json:"title,omitempty"`
	Target     string `json:"target,omitempty"`
	DurationMs int64  `json:"durationMs"`
}

// SwiftTiming is one entry on the TopSwiftFuncs / TopSwiftTypes lists.
// Shape mirrors XCLogParser's SwiftFunctionTime + SwiftTypeCheck.
type SwiftTiming struct {
	File         string  `json:"file,omitempty"`
	Signature    string  `json:"signature,omitempty"`
	StartingLine int     `json:"startingLine,omitempty"`
	Column       int     `json:"column,omitempty"`
	DurationMs   float64 `json:"durationMs,omitempty"`
	Occurrences  int     `json:"occurrences,omitempty"`
}

// LinkerStatistics mirrors XCLogParser's LinkerStatistics (ld64
// -print_statistics). Fields populated only when a step on the activity log
// carried the structured linker-stats attachment or signature.
type LinkerStatistics struct {
	TotalMs                     float64 `json:"totalMs,omitempty"`
	OptionParsingMs             float64 `json:"optionParsingMs,omitempty"`
	OptionParsingPercent        float64 `json:"optionParsingPercent,omitempty"`
	ObjectFileProcessingMs      float64 `json:"objectFileProcessingMs,omitempty"`
	ObjectFileProcessingPercent float64 `json:"objectFileProcessingPercent,omitempty"`
	ResolveSymbolsMs            float64 `json:"resolveSymbolsMs,omitempty"`
	ResolveSymbolsPercent       float64 `json:"resolveSymbolsPercent,omitempty"`
	BuildAtomListMs             float64 `json:"buildAtomListMs,omitempty"`
	BuildAtomListPercent        float64 `json:"buildAtomListPercent,omitempty"`
	RunPassesMs                 float64 `json:"runPassesMs,omitempty"`
	RunPassesPercent            float64 `json:"runPassesPercent,omitempty"`
	WriteOutputMs               float64 `json:"writeOutputMs,omitempty"`
	WriteOutputPercent          float64 `json:"writeOutputPercent,omitempty"`
	Pageins                     int     `json:"pageins,omitempty"`
	Pageouts                    int     `json:"pageouts,omitempty"`
	Faults                      int     `json:"faults,omitempty"`
	ObjectFiles                 int     `json:"objectFiles,omitempty"`
	ObjectFilesBytes            int     `json:"objectFilesBytes,omitempty"`
	ArchiveFiles                int     `json:"archiveFiles,omitempty"`
	ArchiveFilesBytes           int     `json:"archiveFilesBytes,omitempty"`
	DylibFiles                  int     `json:"dylibFiles,omitempty"`
	WroteOutputFileBytes        int     `json:"wroteOutputFileBytes,omitempty"`
}

// CompilationCacheMetrics is the aggregated roll-up emitted by Phase 2 —
// summed across every attachment-bearing section in the invocation.
type CompilationCacheMetrics struct {
	ClangCacheHits   int64 `json:"clangCacheHits,omitempty"`
	ClangCacheMisses int64 `json:"clangCacheMisses,omitempty"`
	SwiftCacheHits   int64 `json:"swiftCacheHits,omitempty"`
	SwiftCacheMisses int64 `json:"swiftCacheMisses,omitempty"`

	Counters     map[string]int64            `json:"counters,omitempty"`
	TaskCounters map[string]map[string]int64 `json:"taskCounters,omitempty"`

	Attachments int `json:"attachments,omitempty"`
}

type BuildSettingsSnapshot struct {
	SwiftOptimizationLevel       string `json:"swiftOptimizationLevel,omitempty"`
	SwiftCompilationMode         string `json:"swiftCompilationMode,omitempty"`
	SwiftWholeModuleOptimization string `json:"swiftWholeModuleOptimization,omitempty"`
	GCCOptimizationLevel         string `json:"gccOptimizationLevel,omitempty"`
	EnableBitcode                string `json:"enableBitcode,omitempty"`
	DebugInformationFormat       string `json:"debugInformationFormat,omitempty"`
	DeploymentTarget             string `json:"deploymentTarget,omitempty"`
	Archs                        string `json:"archs,omitempty"`
	ValidArchs                   string `json:"validArchs,omitempty"`
}

type BuildTimingBucket struct {
	Count        int     `json:"count"`
	TotalSeconds float64 `json:"totalSeconds"`
}

// BuildTimingHistogram is keyed by xcodebuild task type (one bucket per
// observed type from the allow-list in collect.knownBuildTaskTypes).
type BuildTimingHistogram struct {
	Buckets map[string]BuildTimingBucket `json:"buckets,omitempty"`
}
