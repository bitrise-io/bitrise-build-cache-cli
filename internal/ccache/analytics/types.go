package analytics

import "time"

// CcacheStats holds the key statistics parsed from `ccache --print-stats --format=json`.
type CcacheStats struct {
	CacheHitDirect       int     `json:"cacheHitDirect"`
	CacheHitPreprocessed int     `json:"cacheHitPreprocessed"`
	CacheMiss            int     `json:"cacheMiss"`
	CacheHitRate         float64 `json:"cacheHitRate"`
	ErrorsCompiling      int     `json:"errorsCompiling"`
	FilesInCache         int     `json:"filesInCache"`
	CacheSizeKibibyte    int64   `json:"cacheSizeKibibyte"`
}

// Invocation is the analytics payload for the run command, sent for every execution regardless of ccache availability.
type Invocation struct {
	InvocationID         string            `json:"invocationId"`
	InvocationDate       time.Time         `json:"invocationDate"`
	BitriseOrgSlug       string            `json:"bitriseOrgSlug"`
	BitriseAppSlug       string            `json:"bitriseAppSlug"`
	BitriseBuildSlug     string            `json:"bitriseBuildSlug"`
	BitriseStepID        string            `json:"bitriseStepId"`
	Hostname             string            `json:"hostname"`
	Username             string            `json:"username"`
	CommitHash           string            `json:"commitHash"`
	Branch               string            `json:"branch"`
	RepositoryURL        string            `json:"repositoryUrl"`
	CommitEmail          string            `json:"commitEmail"`
	Command              string            `json:"command"`
	FullCommand          string            `json:"fullCommand"`
	DurationMs           int64             `json:"durationMs"`
	Success              bool              `json:"success"`
	Error                string            `json:"error"`
	WorkflowName         string            `json:"workflowName"`
	ProviderID           string            `json:"providerId"`
	CLIVersion           string            `json:"cliVersion"`
	Envs                 map[string]string `json:"envs"`
	OS                   string            `json:"os"`
	HwCPUCores           int               `json:"hwCpuCores"`
	HwMemSize            int64             `json:"hwMemSize"`
	Datacenter           string            `json:"datacenter"`
	DefaultCharset       string            `json:"defaultCharset"`
	Locale               string            `json:"locale"`
	ExternalAppID        string            `json:"externalAppId,omitempty"`
	ExternalBuildID      string            `json:"externalBuildId,omitempty"`
	ExternalWorkflowName string            `json:"externalWorkflowName,omitempty"`
}

// CcacheInvocation is the analytics payload for ccache statistics captured during a run.
// It references the parent Invocation and contains only ccache-specific data.
type CcacheInvocation struct {
	InvocationID        string      `json:"invocationId"`
	ParentInvocationID  string      `json:"parentInvocationId"`
	InvocationDate      time.Time   `json:"invocationDate"`
	CcacheStats         CcacheStats `json:"ccacheStats"`
}
