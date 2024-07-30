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
	LogArchiveDownloaded(duration time.Duration, archiveSizeBytes int64)
	LogArchiveExtracted(duration time.Duration, archiveSizeBytes int64)
	LogMetadataLoaded(duration time.Duration, totalDuration time.Duration, totalFileCount int, restoredFileCount int)
	Wait()
}

type DefaultStepAnalyticsTracker struct {
	tracker analytics.Tracker
	logger  log.Logger
}

func NewStepTracker(stepID string, envProvider func(string) string, logger log.Logger) *DefaultStepAnalyticsTracker {
	p := analytics.Properties{
		"step_id":     stepID,
		"build_slug":  envProvider("BITRISE_BUILD_SLUG"),
		"app_slug":    envProvider("BITRISE_APP_SLUG"),
		"workflow":    envProvider("BITRISE_TRIGGERED_WORKFLOW_ID"),
		"cli_version": envProvider("BITRISE_BUILD_CACHE_CLI_VERSION"),
	}

	return &DefaultStepAnalyticsTracker{
		tracker: analytics.NewDefaultTracker(logger, p),
		logger:  logger,
	}
}

func (t *DefaultStepAnalyticsTracker) LogMetadataSaved(duration time.Duration, fileCount int, size int64) {
	properties := analytics.Properties{
		"duration_ms":         duration.Milliseconds(),
		"file_count":          fileCount,
		"metadata_size_bytes": size,
	}
	t.tracker.Enqueue("step_save_xcode_build_cache_metadata_saved", properties)
}

func (t *DefaultStepAnalyticsTracker) LogDerivedDataUploaded(duration time.Duration, stats UploadFilesStats) {
	properties := analytics.Properties{
		"duration_ms":     duration.Milliseconds(),
		"files_to_upload": stats.FilesToBeUploaded,
		"files_uploaded":  stats.FilesUploded,
		"files_failed":    stats.FilesFailedToUpload,
		"total_files":     stats.TotalFiles,
		"upload_size":     stats.UploadSize,
	}
	t.tracker.Enqueue("step_save_xcode_build_cache_derived_data_uploaded", properties)
}

func (t *DefaultStepAnalyticsTracker) LogSaveFinished(totalDuration time.Duration, err error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}

	properties := analytics.Properties{
		"total_duration_ms": totalDuration.Milliseconds(),
		"error":             errStr,
	}
	t.tracker.Enqueue("step_save_xcode_build_cache_finished", properties)
}

func (t *DefaultStepAnalyticsTracker) LogArchiveDownloaded(duration time.Duration, archiveSizeBytes int64) {
	properties := analytics.Properties{
		"duration_ms":        duration.Milliseconds(),
		"archive_size_bytes": archiveSizeBytes,
	}
	t.tracker.Enqueue("step_restore_xcode_build_cache_archive_downloaded", properties)
}

func (t *DefaultStepAnalyticsTracker) LogArchiveExtracted(duration time.Duration, archiveSizeBytes int64) {
	properties := analytics.Properties{
		"duration_ms":        duration.Milliseconds(),
		"archive_size_bytes": archiveSizeBytes,
	}
	t.tracker.Enqueue("step_restore_xcode_build_cache_archive_extracted", properties)
}

func (t *DefaultStepAnalyticsTracker) LogMetadataLoaded(duration time.Duration, totalDuration time.Duration, totalFileCount int, restoredFileCount int) {
	properties := analytics.Properties{
		"duration_ms":         duration.Milliseconds(),
		"total_duration_ms":   totalDuration.Milliseconds(),
		"total_file_count":    totalFileCount,
		"restored_file_count": restoredFileCount,
	}
	t.tracker.Enqueue("step_restore_xcode_build_cache_metadata_loaded", properties)
}

func (t *DefaultStepAnalyticsTracker) Wait() {
	t.tracker.Wait()
}
