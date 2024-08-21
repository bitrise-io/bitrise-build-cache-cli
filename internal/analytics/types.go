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
	OperationID   string    `json:"operationId"`
	OperationType string    `json:"operationType"`
	StartedAt     time.Time `json:"startedAt"`
	Duration      int       `json:"durationMs"`
	TransferSize  int64     `json:"transferSizeBytes"`
	CacheKey      string    `json:"cacheKey"`
	CacheKeyType  *string   `json:"cacheKeyType,omitempty"`
	Error         *string   `json:"error,omitempty"`
	CIProvider    string    `json:"ciProvider"`
	ProjectID     *string   `json:"projectId,omitempty"`
	BuildID       *string   `json:"buildId,omitempty"`
	RepositoryURL *string   `json:"repositoryUrl,omitempty"`
	CommitHash    string    `json:"commitHash"`
	Branch        *string   `json:"branch,omitempty"`
	WorkflowID    *string   `json:"workflowId,omitempty"`
	WorkflowTitle *string   `json:"workflowTitle,omitempty"`
	CLIVersion    string    `json:"cliVersion"`
	FileStats     FileStats `json:"fileStats"`
}
