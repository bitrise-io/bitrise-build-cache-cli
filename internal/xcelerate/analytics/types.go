package analytics

import "time"

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
	InvocationID     string            `json:"invocationId"`
	InvocationDate   time.Time         `json:"invocationDate"`
	BitriseOrgSlug   string            `json:"bitriseOrgSlug"`
	BitriseAppSlug   string            `json:"bitriseAppSlug"`
	BitriseBuildSlug string            `json:"bitriseBuildSlug"`
	BitriseStepID    string            `json:"bitriseStepId"`
	Hostname         string            `json:"hostname"`
	Username         string            `json:"username"`
	CommitHash       string            `json:"commitHash"`
	Branch           string            `json:"branch"`
	RepositoryURL    string            `json:"repositoryUrl"`
	CommitEmail      string            `json:"commitEmail"`
	Command          string            `json:"command"`
	FullCommand      string            `json:"fullCommand"`
	DurationMs       int64             `json:"durationMs"`
	HitRate          float32           `json:"hitRate"`
	Success          bool              `json:"success"`
	Error            string            `json:"error"`
	XcodeVersion     string            `json:"xcodeVersion"`
	WorkflowName     string            `json:"workflowName"`
	ProviderID       string            `json:"providerId"`
	CLIVersion       string            `json:"cliVersion"`
	Envs             map[string]string `json:"envs"`
	OS               string            `json:"os"`
	HwCPUCores       int               `json:"hwCpuCores"`
	HwMemSize        int64             `json:"hwMemSize"`
	Datacenter       string            `json:"datacenter"`
	DefaultCharset   string            `json:"defaultCharset"`
	Locale           string            `json:"locale"`
}
