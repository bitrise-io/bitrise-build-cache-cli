// Package payload owns the serialised xcprobe enrichment payload type and
// the one-way edge onto analytics.Invocation. analytics never imports back —
// the Invocation field is a json.RawMessage so the cycle is impossible.
package payload

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/host"
)

// EnrichmentPayloadSchemaVersion is bumped only when a field's meaning
// changes; purely additive fields do not require a bump because the BE
// tolerates unknown JSON keys.
const EnrichmentPayloadSchemaVersion = 1

// EnrichmentPayload is the top-level envelope attached to Invocation.Enrichment.
type EnrichmentPayload struct {
	SchemaVersion   int               `json:"schemaVersion"`
	CollectorErrors map[string]string `json:"collectorErrors,omitempty"`

	Host          *host.Snapshot         `json:"host,omitempty"`
	BuildSettings *BuildSettingsSnapshot `json:"buildSettings,omitempty"`
	BuildTimings  *BuildTimingHistogram  `json:"buildTimings,omitempty"`
}

// BuildSettingsSnapshot holds the small set of build settings the Phase 0
// collector plucks from `xcodebuild -showBuildSettings -json`.
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

// BuildTimingBucket is one row of the xcodebuild -showBuildTimingSummary tail.
type BuildTimingBucket struct {
	Count        int     `json:"count"`
	TotalSeconds float64 `json:"totalSeconds"`
}

// BuildTimingHistogram is keyed by xcodebuild task type (CompileSwift,
// CompileC, Ld, PhaseScriptExecution, CodeSign, CopySwiftLibs,
// CompileAssetCatalog, CompileStoryboard). One bucket per observed type.
type BuildTimingHistogram struct {
	Buckets map[string]BuildTimingBucket `json:"buckets,omitempty"`
}
