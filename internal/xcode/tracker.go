package xcode

import (
	"time"

	"github.com/bitrise-io/go-utils/v2/analytics"
	"github.com/bitrise-io/go-utils/v2/log"
)

//go:generate moq -out mocks/tracker_mock.go -pkg mocks . StepAnalyticsTracker
type StepAnalyticsTracker interface {
	LogMetadataSaved(duration time.Duration, fileCount int, size int64)
	LogDerivedDataUploaded(duration time.Duration, stats UploadFilesStats)
	LogSaveFinished(totalDuration time.Duration, err error)
	LogMetadataLoaded(duration time.Duration, cacheKeyType string, totalFileCount int, restoredFileCount int, size int64)
	LogDerivedDataDownloaded(duration time.Duration, stats DownloadFilesStats)
	LogRestoreFinished(totalDuration time.Duration, err error)

	Wait()
}

type DefaultStepAnalyticsTracker struct {
	tracker    analytics.Tracker
	logger     log.Logger
	cliVersion string
}

func NewDefaultStepTracker(stepID string, envProvider func(string) string, logger log.Logger) *DefaultStepAnalyticsTracker {
	p := analytics.Properties{
		"step_id":     stepID,
		"build_slug":  envProvider("BITRISE_BUILD_SLUG"),
		"app_slug":    envProvider("BITRISE_APP_SLUG"),
		"workflow":    envProvider("BITRISE_TRIGGERED_WORKFLOW_ID"),
		"cli_version": envProvider("BITRISE_BUILD_CACHE_CLI_VERSION"),
	}

	return &DefaultStepAnalyticsTracker{
		tracker:    analytics.NewDefaultTracker(logger, p),
		logger:     logger,
		cliVersion: envProvider("BITRISE_BUILD_CACHE_CLI_VERSION"),
	}
}

func (t *DefaultStepAnalyticsTracker) LogMetadataSaved(duration time.Duration, fileCount int, size int64) {
	properties := t.propertiesWithCLIVersion().Merge(analytics.Properties{
		"duration_ms":         duration.Milliseconds(),
		"file_count":          fileCount,
		"metadata_size_bytes": size,
	})
	t.tracker.Enqueue("step_save_xcode_build_cache_metadata_saved", properties)
}

func (t *DefaultStepAnalyticsTracker) LogDerivedDataUploaded(duration time.Duration, stats UploadFilesStats) {
	properties := t.propertiesWithCLIVersion().Merge(analytics.Properties{
		"duration_ms":             duration.Milliseconds(),
		"files_to_upload":         stats.FilesToUpload,
		"files_uploaded":          stats.FilesUploaded,
		"files_failed":            stats.FilesFailedToUpload,
		"total_files":             stats.TotalFiles,
		"upload_size_bytes":       stats.UploadSize,
		"largest_file_size_bytes": stats.LargestFileSize,
	})
	t.tracker.Enqueue("step_save_xcode_build_cache_derived_data_uploaded", properties)
}

func (t *DefaultStepAnalyticsTracker) LogSaveFinished(totalDuration time.Duration, err error) {
	properties := t.propertiesWithCLIVersion().Merge(analytics.Properties{
		"total_duration_ms": totalDuration.Milliseconds(),
	})
	if err != nil {
		properties["error"] = err.Error()
	}
	t.tracker.Enqueue("step_save_xcode_build_cache_finished", properties)
}

func (t *DefaultStepAnalyticsTracker) LogMetadataLoaded(duration time.Duration, cacheKeyType string, totalFileCount int, restoredFileCount int, size int64) {
	properties := t.propertiesWithCLIVersion().Merge(analytics.Properties{
		"duration_ms":         duration.Milliseconds(),
		"cache_key_type":      cacheKeyType,
		"total_file_count":    totalFileCount,
		"restored_file_count": restoredFileCount,
		"metadata_size_bytes": size,
	})
	t.tracker.Enqueue("step_restore_xcode_build_cache_metadata_loaded", properties)
}

func (t *DefaultStepAnalyticsTracker) LogDerivedDataDownloaded(duration time.Duration, stats DownloadFilesStats) {
	properties := t.propertiesWithCLIVersion().Merge(analytics.Properties{
		"duration_ms":             duration.Milliseconds(),
		"files_to_download":       stats.FilesToBeDownloaded,
		"files_downloaded":        stats.FilesDownloaded,
		"files_missing":           stats.FilesMissing,
		"files_failed":            stats.FilesFailedToDownload,
		"download_size_bytes":     stats.DownloadSize,
		"largest_file_size_bytes": stats.LargestFileSize,
	})
	t.tracker.Enqueue("step_restore_xcode_build_cache_derived_data_downloaded", properties)
}

func (t *DefaultStepAnalyticsTracker) LogRestoreFinished(totalDuration time.Duration, err error) {
	properties := t.propertiesWithCLIVersion().Merge(analytics.Properties{
		"total_duration_ms": totalDuration.Milliseconds(),
	})
	if err != nil {
		properties["error"] = err.Error()
	}
	t.tracker.Enqueue("step_restore_xcode_build_cache_finished", properties)
}

func (t *DefaultStepAnalyticsTracker) Wait() {
	t.tracker.Wait()
}

func (t *DefaultStepAnalyticsTracker) propertiesWithCLIVersion() analytics.Properties {
	if t.cliVersion == "" {
		return analytics.Properties{}
	}

	return analytics.Properties{
		"cli_version": t.cliVersion,
	}
}
