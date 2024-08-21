package cmd

import (
	xa "github.com/bitrise-io/bitrise-build-cache-cli/internal/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcode"
	"github.com/google/uuid"
	"time"
)

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
