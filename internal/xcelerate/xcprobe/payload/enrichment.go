// Package payload is the xcprobe enrichment payload type and the one-way
// edge onto analytics.Invocation (via json.RawMessage, so analytics cannot
// import back).
package payload

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/host"
)

// EnrichmentPayloadSchemaVersion must be bumped only when a field's meaning
// changes; purely additive fields are safe because the BE tolerates unknown
// JSON keys.
const EnrichmentPayloadSchemaVersion = 1

// EnrichmentPayload is the top-level envelope attached to Invocation.Enrichment.
type EnrichmentPayload struct {
	SchemaVersion   int               `json:"schemaVersion"`
	CollectorErrors map[string]string `json:"collectorErrors,omitempty"`

	Host          *host.Snapshot         `json:"host,omitempty"`
	BuildSettings *BuildSettingsSnapshot `json:"buildSettings,omitempty"`
	BuildTimings  *BuildTimingHistogram  `json:"buildTimings,omitempty"`
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
