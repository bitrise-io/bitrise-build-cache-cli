//go:build unit

package ccache

import (
	"testing"

	utilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
)

func TestCacheEffectiveness_SummaryLine(t *testing.T) {
	t.Run("mixed hits and misses", func(t *testing.T) {
		e := CacheEffectiveness{Hits: 3, Total: 4, DownloadBytes: 2048, UploadBytes: 4096}

		assert.Equal(t, "Ccache stats: hits: 3 (2.0 kB) / total: 4 (75.00%). Uploaded: 4.1 kB", e.SummaryLine())
	})

	t.Run("no calls at all", func(t *testing.T) {
		assert.Equal(t, "Ccache stats: hits: 0 (0 B) / total: 0 (0.00%). Uploaded: 0 B", CacheEffectiveness{}.SummaryLine())
	})

	t.Run("errors are appended", func(t *testing.T) {
		e := CacheEffectiveness{Hits: 1, Total: 2, Errors: 3}

		assert.Contains(t, e.SummaryLine(), "Remote storage errors: 3.")
	})

	t.Run("negative byte counts do not wrap around", func(t *testing.T) {
		e := CacheEffectiveness{Total: 1, DownloadBytes: -1, UploadBytes: -1}

		assert.Equal(t, "Ccache stats: hits: 0 (0 B) / total: 1 (0.00%). Uploaded: 0 B", e.SummaryLine())
	})
}

func TestCacheEffectiveness_WriteOnlyWarning(t *testing.T) {
	t.Run("write-only run", func(t *testing.T) {
		e := CacheEffectiveness{Total: 1284, UploadBytes: 139500000}

		assert.Contains(t, e.WriteOnlyWarning(), "1284 lookups, all misses")
		assert.Contains(t, e.WriteOnlyWarning(), "140 MB")
	})

	t.Run("silent when anything was reused", func(t *testing.T) {
		assert.Empty(t, CacheEffectiveness{Hits: 1, Total: 1284, UploadBytes: 100}.WriteOnlyWarning())
	})

	t.Run("silent without uploads", func(t *testing.T) {
		assert.Empty(t, CacheEffectiveness{Total: 10}.WriteOnlyWarning())
	})

	t.Run("silent without lookups", func(t *testing.T) {
		assert.Empty(t, CacheEffectiveness{UploadBytes: 100}.WriteOnlyWarning())
	})
}

func TestCacheEffectiveness_Log(t *testing.T) {
	t.Run("logs the summary and warns on a write-only invocation", func(t *testing.T) {
		logger := &utilsMocks.Logger{}
		registerLoggerMethod(logger, "TInfof")
		registerLoggerMethod(logger, "TWarnf")

		CacheEffectiveness{Total: 10, UploadBytes: 1024}.Log(logger)

		logger.AssertNumberOfCalls(t, "TInfof", 1)
		logger.AssertNumberOfCalls(t, "TWarnf", 1)
	})

	t.Run("stays silent when the invocation never touched the cache", func(t *testing.T) {
		logger := &utilsMocks.Logger{}

		CacheEffectiveness{}.Log(logger)

		logger.AssertNumberOfCalls(t, "TInfof", 0)
	})
}
