// Package payload owns the serialised xcprobe enrichment payload type and
// the one-way edge onto analytics.Invocation. analytics never imports back —
// the Invocation field is a json.RawMessage so the cycle is impossible.
package payload

// EnrichmentPayloadSchemaVersion is bumped only when a field's meaning
// changes; purely additive fields do not require a bump because the BE
// tolerates unknown JSON keys.
const EnrichmentPayloadSchemaVersion = 1

// EnrichmentPayload is the top-level envelope attached to Invocation.Enrichment.
type EnrichmentPayload struct {
	SchemaVersion   int               `json:"schemaVersion"`
	CollectorErrors map[string]string `json:"collectorErrors,omitempty"`

	Host          *HostSnapshot          `json:"host,omitempty"`
	BuildSettings *BuildSettingsSnapshot `json:"buildSettings,omitempty"`
	BuildTimings  *BuildTimingHistogram  `json:"buildTimings,omitempty"`
}

// HostSnapshot mirrors the shape of host.Snapshot for serialisation; kept
// decoupled from the host package so the on-disk/wire format stays stable
// across probe-side refactors.
type HostSnapshot struct {
	OS             string `json:"os,omitempty"`
	CPUCores       int    `json:"cpuCores,omitempty"`
	MemSize        int64  `json:"memSize,omitempty"`
	Locale         string `json:"locale,omitempty"`
	DefaultCharset string `json:"defaultCharset,omitempty"`
	Hostname       string `json:"hostname,omitempty"`
	Username       string `json:"username,omitempty"`

	ThermalPressure   string `json:"thermalPressure,omitempty"`
	MemFreeMB         uint64 `json:"memFreeMB,omitempty"`
	SwapTotalMB       uint64 `json:"swapTotalMB,omitempty"`
	SwapFreeMB        uint64 `json:"swapFreeMB,omitempty"`
	UptimeSeconds     uint64 `json:"uptimeSeconds,omitempty"`
	IsVirtual         bool   `json:"isVirtual,omitempty"`
	SpotlightActive   bool   `json:"spotlightActive,omitempty"`
	TimeMachineActive bool   `json:"timeMachineActive,omitempty"`
	ModelID           string `json:"modelID,omitempty"`
	Chip              string `json:"chip,omitempty"`
}

// BuildSettingsSnapshot holds the small set of build settings the Phase 0
// collector plucks from `xcodebuild -showBuildSettings -json`.
type BuildSettingsSnapshot struct {
	SwiftOptimizationLevel        string `json:"swiftOptimizationLevel,omitempty"`
	SwiftCompilationMode          string `json:"swiftCompilationMode,omitempty"`
	SwiftWholeModuleOptimization  string `json:"swiftWholeModuleOptimization,omitempty"`
	GCCOptimizationLevel          string `json:"gccOptimizationLevel,omitempty"`
	EnableBitcode                 string `json:"enableBitcode,omitempty"`
	DebugInformationFormat        string `json:"debugInformationFormat,omitempty"`
	DeploymentTarget              string `json:"deploymentTarget,omitempty"`
	Archs                         string `json:"archs,omitempty"`
	ValidArchs                    string `json:"validArchs,omitempty"`
}

// BuildTimingBucket is one row of the xcodebuild -showBuildTimingSummary tail.
type BuildTimingBucket struct {
	Count         int     `json:"count"`
	TotalSeconds  float64 `json:"totalSeconds"`
}

// BuildTimingHistogram is keyed by xcodebuild task type (CompileSwift,
// CompileC, Ld, PhaseScriptExecution, CodeSign, CopySwiftLibs,
// CompileAssetCatalog, CompileStoryboard). One bucket per observed type.
type BuildTimingHistogram struct {
	Buckets map[string]BuildTimingBucket `json:"buckets,omitempty"`
}
