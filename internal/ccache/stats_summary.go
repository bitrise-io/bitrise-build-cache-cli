package ccache

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/dustin/go-humanize"
)

// CacheEffectiveness is the cache result of a single ccache invocation, filled either from the
// storage helper's own request outcomes or from ccache's own statistics.
type CacheEffectiveness struct {
	Hits          int64
	Total         int64
	Errors        int64
	DownloadBytes int64
	UploadBytes   int64
}

// Log makes a write-only cache visible from the build log instead of only from analytics.
func (e CacheEffectiveness) Log(logger log.Logger) {
	if e == (CacheEffectiveness{}) {
		return
	}

	logger.TInfof("%s", e.SummaryLine())

	if warning := e.WriteOnlyWarning(); warning != "" {
		logger.TWarnf("%s", warning)
	}
}

func (e CacheEffectiveness) HitRate() float64 {
	if e.Total == 0 {
		return 0
	}

	return float64(e.Hits) / float64(e.Total)
}

func (e CacheEffectiveness) SummaryLine() string {
	line := fmt.Sprintf(
		"Ccache stats: hits: %d (%s) / total: %d (%.02f%%). Uploaded: %s",
		e.Hits,
		humanize.Bytes(clampToUint64(e.DownloadBytes)),
		e.Total,
		e.HitRate()*100,
		humanize.Bytes(clampToUint64(e.UploadBytes)),
	)

	if e.Errors > 0 {
		line += fmt.Sprintf(" Remote storage errors: %d.", e.Errors)
	}

	return line
}

// WriteOnlyWarning flags cache keys that do not survive from one build to the next.
func (e CacheEffectiveness) WriteOnlyWarning() string {
	if e.Hits > 0 || e.Total == 0 || e.UploadBytes <= 0 {
		return ""
	}

	return fmt.Sprintf(
		"Ccache reused nothing from the cache in this invocation (%d lookups, all misses) while uploading %s. "+
			"If this repeats across builds, the cache keys are not stable between builds.",
		e.Total,
		humanize.Bytes(clampToUint64(e.UploadBytes)),
	)
}

func clampToUint64(value int64) uint64 {
	if value < 0 {
		return 0
	}

	return uint64(value)
}
