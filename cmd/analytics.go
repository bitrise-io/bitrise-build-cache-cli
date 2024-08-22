package cmd

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	xa "github.com/bitrise-io/bitrise-build-cache-cli/internal/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcode"
	"github.com/bitrise-io/go-utils/v2/log"
)

func sendCacheOperationAnalytics(op *xa.CacheOperation, cmdError error, logger log.Logger, authConfig common.CacheAuthConfig) {
	op.Duration = int(time.Since(op.StartedAt).Milliseconds())

	if cmdError != nil {
		errStr := cmdError.Error()
		op.Error = &errStr
	}

	xaClint, clientErr := xa.NewClient(consts.AnalyticsServiceEndpoint, authConfig.AuthToken, logger)
	if clientErr != nil {
		logger.Warnf("Failed to create Xcode Analytics Service client: %s", clientErr)
	} else {
		if payload, err := json.Marshal(op); err != nil {
			logger.Debugf("Sending cache operation to Xcode Analytics Service: %s", string(payload))
		}

		if err := xaClint.PutCacheOperation(op); err != nil {
			logger.Warnf("Failed to send cache operation to Xcode Analytics Service: %s", err)
		}
	}
}

func newCacheOperation(startT time.Time, operationType, cacheKey string, envProvider func(string) string) *xa.CacheOperation {
	op := &xa.CacheOperation{
		OperationID:   uuid.NewString(),
		OperationType: operationType,
		StartedAt:     startT,
		CacheKey:      cacheKey,
		CIProvider:    "bitrise",
		CLIVersion:    envProvider("BITRISE_BUILD_CACHE_CLI_VERSION"),
		CommitHash:    envProvider("BITRISE_GIT_COMMIT"),
	}

	projectID := envProvider("BITRISE_APP_SLUG")
	if projectID != "" {
		op.ProjectID = &projectID
	}

	buildID := envProvider("BITRISE_BUILD_SLUG")
	if buildID != "" {
		op.BuildID = &buildID
	}

	workflowID := envProvider("BITRISE_TRIGGERED_WORKFLOW_ID")
	if workflowID != "" {
		op.WorkflowID = &workflowID
	}

	workflowTitle := envProvider("BITRISE_TRIGGERED_WORKFLOW_TITLE")
	if workflowTitle != "" {
		op.WorkflowTitle = &workflowTitle
	}

	repoURL := envProvider("GIT_REPOSITORY_URL")
	if repoURL != "" {
		op.RepositoryURL = &repoURL
	}

	branch := envProvider("BITRISE_GIT_BRANCH")
	if branch != "" {
		op.Branch = &branch
	}

	return op
}

func fillCacheOperationWithUploadStats(op *xa.CacheOperation, stats xcode.UploadFilesStats) {
	op.TransferSize = stats.UploadSize
	op.FileStats = xa.FileStats{
		FilesToTransfer:  stats.FilesToUpload,
		FilesTransferred: stats.FilesUploaded,
		FilesFailed:      stats.FilesFailedToUpload,
		FilesMissing:     stats.FilesToUpload,
		TotalFiles:       stats.TotalFiles,
	}
}

func fillCacheOperationWithDownloadStats(op *xa.CacheOperation, stats xcode.DownloadFilesStats) {
	op.TransferSize = stats.DownloadSize
	op.FileStats = xa.FileStats{
		FilesToTransfer:  stats.FilesToBeDownloaded,
		FilesTransferred: stats.FilesDownloaded,
		FilesFailed:      stats.FilesFailedToDownload,
		FilesMissing:     stats.FilesMissing,
		TotalFiles:       stats.FilesToBeDownloaded,
	}
}
