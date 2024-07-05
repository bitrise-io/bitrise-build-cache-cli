package xcode

import (
	"time"

	"github.com/bitrise-io/go-utils/v2/analytics"
	"github.com/bitrise-io/go-utils/v2/log"
)

type StepAnalyticsTracker struct {
	tracker analytics.Tracker
	logger  log.Logger
}

func NewStepTracker(stepID string, envProvider func(string) string, logger log.Logger) StepAnalyticsTracker {
	p := analytics.Properties{
		"step_id":     stepID,
		"build_slug":  envProvider("BITRISE_BUILD_SLUG"),
		"app_slug":    envProvider("BITRISE_APP_SLUG"),
		"workflow":    envProvider("BITRISE_TRIGGERED_WORKFLOW_ID"),
		"cli_version": envProvider("BITRISE_BUILD_CACHE_CLI_VERSION"),
	}

	return StepAnalyticsTracker{
		tracker: analytics.NewDefaultTracker(logger, p),
		logger:  logger,
	}
}

func (t *StepAnalyticsTracker) LogMetadataSaved(duration time.Duration, fileCount int) {
	properties := analytics.Properties{
		"duration_ms": duration.Milliseconds(),
		"file_count":  fileCount,
	}
	t.tracker.Enqueue("step_save_xcode_build_cache_metadata_saved", properties)
}

func (t *StepAnalyticsTracker) LogArchiveCreated(duration time.Duration, archiveSizeBytes int64) {
	properties := analytics.Properties{
		"duration_ms":        duration.Milliseconds(),
		"archive_size_bytes": archiveSizeBytes,
	}
	t.tracker.Enqueue("step_save_xcode_build_cache_archive_created", properties)
}

func (t *StepAnalyticsTracker) LogArchiveUploaded(duration time.Duration, totalDuration time.Duration, archiveSizeBytes int64) {
	properties := analytics.Properties{
		"duration_ms":        duration.Milliseconds(),
		"total_duration_ms":  totalDuration.Milliseconds(),
		"archive_size_bytes": archiveSizeBytes,
	}
	t.tracker.Enqueue("step_save_xcode_build_cache_archive_uploaded", properties)
}

func (t *StepAnalyticsTracker) LogArchiveDownloaded(duration time.Duration, archiveSizeBytes int64) {
	properties := analytics.Properties{
		"duration_ms":        duration.Milliseconds(),
		"archive_size_bytes": archiveSizeBytes,
	}
	t.tracker.Enqueue("step_restore_xcode_build_cache_archive_downloaded", properties)
}

func (t *StepAnalyticsTracker) LogArchiveExtracted(duration time.Duration, archiveSizeBytes int64) {
	properties := analytics.Properties{
		"duration_ms":        duration.Milliseconds(),
		"archive_size_bytes": archiveSizeBytes,
	}
	t.tracker.Enqueue("step_restore_xcode_build_cache_archive_extracted", properties)
}

func (t *StepAnalyticsTracker) LogMetadataLoaded(duration time.Duration, totalDuration time.Duration, totalFileCount int, restoredFileCount int) {
	properties := analytics.Properties{
		"duration_ms":         duration.Milliseconds(),
		"total_duration_ms":   totalDuration.Milliseconds(),
		"total_file_count":    totalFileCount,
		"restored_file_count": restoredFileCount,
	}
	t.tracker.Enqueue("step_restore_xcode_build_cache_metadata_loaded", properties)
}

func (t *StepAnalyticsTracker) Wait() {
	t.tracker.Wait()
}
