package xcodeargs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Line shapes taken from a real WordPress-iOS build.
func TestHitRateCapturer_CountsCASErrors(t *testing.T) {
	var stats RunStats
	capture := (&DefaultRunner{}).hitRateCapturer(&stats)

	for _, line := range []string{
		"warning: CAS error: deadlineExceeded(connectionError: nil)",
		"warning: CAS error: deadlineExceeded(connectionError: nil)",
		"CompileSwift normal arm64 /Users/me/App/Foo.swift",
		"warning: CAS error: unavailable(connectionError: nil)",
		"note: 12 hits / 40 cacheable tasks",
		"warning: add '@preconcurrency' to suppress 'Sendable'-related warnings",
	} {
		capture(line)
	}

	assert.Equal(t, int64(3), stats.CacheStats.CASErrors)
	assert.Equal(t, int64(12), stats.CacheStats.Hits, "the existing stats parsing must still work")
	assert.Equal(t, int64(40), stats.CacheStats.TotalTasks)
}

func TestHitRateCapturer_ColdCacheHasNoCASErrors(t *testing.T) {
	var stats RunStats
	capture := (&DefaultRunner{}).hitRateCapturer(&stats)

	for _, line := range []string{
		"note: 0 hits / 2746 cacheable tasks",
		"CompileSwift normal arm64 /Users/me/App/Bar.swift",
		"** BUILD SUCCEEDED **",
	} {
		capture(line)
	}

	assert.Zero(t, stats.CacheStats.CASErrors)
	assert.Equal(t, int64(2746), stats.CacheStats.TotalTasks)
}
