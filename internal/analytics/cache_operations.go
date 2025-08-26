package analytics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/google/uuid"
	"github.com/hashicorp/go-retryablehttp"
)

func NewCacheOperation(startT time.Time, operationType string, metadata *common.CacheConfigMetadata) *CacheOperation {
	op := &CacheOperation{
		OperationID:   uuid.NewString(),
		OperationType: operationType,
		StartedAt:     startT,
		CIProvider:    metadata.CIProvider,
		CLIVersion:    metadata.CLIVersion,
		CommitHash:    metadata.GitMetadata.CommitHash,
	}

	op.ProjectID = &metadata.BitriseAppID
	op.BuildID = &metadata.BitriseBuildID
	op.WorkflowTitle = &metadata.BitriseWorkflowName
	op.RepositoryURL = &metadata.GitMetadata.RepoURL
	op.Branch = &metadata.GitMetadata.Branch

	return op
}

func (c *Client) PutCacheOperation(op *CacheOperation) error {
	requestURL := fmt.Sprintf("%s/operations/%s", c.baseURL, op.OperationID)

	payload, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("failed to marshal cache operation: %w", err)
	}

	req, err := retryablehttp.NewRequest(http.MethodPut, requestURL, payload)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.accessToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to perform HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return unwrapError(resp)
	}

	return nil
}

func (op *CacheOperation) FillWithUploadStats(stats kv.UploadFilesStats) {
	op.TransferSize = stats.UploadSize
	op.FileStats = FileStats{
		FilesToTransfer:  stats.FilesToUpload,
		FilesTransferred: stats.FilesUploaded,
		FilesFailed:      stats.FilesFailedToUpload,
		FilesMissing:     stats.FilesToUpload,
		TotalFiles:       stats.TotalFiles,
	}
}

func (op *CacheOperation) FillWithDownloadStats(stats kv.DownloadFilesStats) {
	op.TransferSize = stats.DownloadSize
	op.FileStats = FileStats{
		FilesToTransfer:  stats.FilesToBeDownloaded,
		FilesTransferred: stats.FilesDownloaded,
		FilesFailed:      stats.FilesFailedToDownload,
		FilesMissing:     stats.FilesMissing,
		TotalFiles:       stats.FilesToBeDownloaded,
	}
}
