package analytics

import (
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
)

const (
	OperationTypeUpload   = "upload"
	OperationTypeDownload = "download"
)

type FileStats struct {
	FilesToTransfer  int `json:"filesToTransfer"`
	FilesTransferred int `json:"filesTransferred"`
	FilesFailed      int `json:"filesFailed"`
	FilesMissing     int `json:"filesMissing"`
	TotalFiles       int `json:"totalFiles"`
}

type CacheOperation struct {
	OperationID          string    `json:"operationId"`
	OperationType        string    `json:"operationType"`
	StartedAt            time.Time `json:"startedAt"`
	DurationMilliseconds int       `json:"durationMs"`
	TransferSize         int64     `json:"transferSizeBytes"`
	CacheKey             string    `json:"cacheKey"`
	CacheKeyType         *string   `json:"cacheKeyType,omitempty"`
	Error                *string   `json:"error,omitempty"`
	CIProvider           string    `json:"ciProvider"`
	ProjectID            *string   `json:"projectId,omitempty"`
	BuildID              *string   `json:"buildId,omitempty"`
	RepositoryURL        *string   `json:"repositoryUrl,omitempty"`
	CommitHash           string    `json:"commitHash"`
	Branch               *string   `json:"branch,omitempty"`
	WorkflowID           *string   `json:"workflowId,omitempty"`
	WorkflowTitle        *string   `json:"workflowTitle,omitempty"`
	CLIVersion           string    `json:"cliVersion"`
	FileStats            FileStats `json:"fileStats"`
}

type Invocation struct {
	InvocationID         string              `json:"invocationId"`
	InvocationDate       time.Time           `json:"invocationDate"`
	BitriseOrgSlug       string              `json:"bitriseOrgSlug"`
	BitriseAppSlug       string              `json:"bitriseAppSlug"`
	BitriseBuildSlug     string              `json:"bitriseBuildSlug"`
	BitriseStepID        string              `json:"bitriseStepId"`
	Hostname             string              `json:"hostname"`
	Username             string              `json:"username"`
	CommitHash           string              `json:"commitHash"`
	Branch               string              `json:"branch"`
	RepositoryURL        string              `json:"repositoryUrl"`
	CommitEmail          string              `json:"commitEmail"`
	Command              string              `json:"command"`
	FullCommand          string              `json:"fullCommand"`
	DurationMs           int64               `json:"durationMs"`
	HitRate              float32             `json:"hitRate"`
	Success              bool                `json:"success"`
	Error                string              `json:"error"`
	XcodeVersion         string              `json:"xcodeVersion"`
	WorkflowName         string              `json:"workflowName"`
	ProviderID           string              `json:"providerId"`
	CLIVersion           string              `json:"cliVersion"`
	Envs                 map[string]string   `json:"envs"`
	OS                   string              `json:"os"`
	HwCPUCores           int                 `json:"hwCpuCores"`
	HwMemSize            int64               `json:"hwMemSize"`
	Datacenter           string              `json:"datacenter"`
	DefaultCharset       string              `json:"defaultCharset"`
	Locale               string              `json:"locale"`
	ToolBuildNumber      string              `json:"toolBuildNumber"`
	ExternalAppID        string              `json:"externalAppId,omitempty"`
	ExternalBuildID      string              `json:"externalBuildId,omitempty"`
	ExternalWorkflowName string              `json:"externalWorkflowName,omitempty"`
	BenchmarkPhase       string              `json:"benchmarkPhase,omitempty"`
	CacheBlobStats       *blobstats.Snapshot `json:"cacheBlobStats,omitempty"`
	// Populated from `xcrun xcresulttool get build-results` on the wrapper self-enrich path.
	Targets  []TargetSummary  `json:"targets,omitempty"`
	Failures []FailureSummary `json:"failures,omitempty"`

	// DsymutilCasShim carries the per-xcodebuild dsymutil shim telemetry: whether
	// the shim is installed on disk, whether it was invoked at all (summed over
	// every dsymutil call this xcodebuild run issued), and the missed /
	// filtered-stderr totals. Missed is the ground-truth signal for "did the fix
	// work?" — zero means every CAS id resolved under the plugin.
	DsymutilCasShim *DsymutilCasShimStats `json:"dsymutilCasShim,omitempty"`
}

type DsymutilCasShimStats struct {
	Installed         bool  `json:"installed"`
	InvocationCount   int   `json:"invocationCount,omitempty"`
	BypassCount       int   `json:"bypassCount,omitempty"`
	Missed            int   `json:"missed,omitempty"`
	FilteredStderrLns int   `json:"filteredStderrLns,omitempty"`
	TotalDurationMs   int64 `json:"totalDurationMs,omitempty"`
}

type TargetSummary struct {
	Name            string `json:"name"`
	BuildDurationMs int64  `json:"buildDurationInMs,omitempty"`
}

type FailureSummary struct {
	TargetName string `json:"targetName,omitempty"`
	Message    string `json:"message"`
}
