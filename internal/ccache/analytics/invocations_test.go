//go:build unit

package analytics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// sampleStatsOutput is representative `ccache -v -v -s` text output.
const sampleStatsOutput = `Cache directory:                          /home/user/.cache/ccache
Config file:                              /home/user/.config/ccache/ccache.conf
Stats updated:                            Mon Apr 14 10:00:00 2025
Stats zeroed:                             Mon Apr 14 09:00:00 2025
Cacheable calls:                            18
  Hits:                                      5 / 18  (27.78 %)
    Direct:                                  3
    Preprocessed:                            2
  Misses:                                   13
Uncacheable calls:                           2
  Autoconf compile/link:                     0
  Bad compiler arguments:                    0
  Called for linking:                        2
  Called for preprocessing:                  0
  Ccache disabled:                           0
  Compilation failed:                        0
  Compiler output file missing:              0
  Compiler produced empty output:            0
  Compiler produced stdout:                  0
  Could not use modules:                     0
  Could not use precompiled header:          0
  Forced recache:                            0
  Multiple source files:                     0
  No input file:                             0
  Output to stdout:                          0
  Preprocessing failed:                      0
  Unsupported code directive:                0
  Unsupported compiler option:               0
  Unsupported environment variable:          0
  Unsupported source encoding:               0
  Unsupported source language:               0
Errors:                                      0
  Compiler check failed:                     0
  Could not find compiler:                   0
  Could not read or parse input file:        0
  Could not write to output file:            0
  Error hashing extra file:                  0
  Input file modified during compilation:    0
  Internal error:                            0
  Missing cache file:                        0
Local storage:
  Cache size (GiB):                        0.4 / 10.0 ( 3.85%)
  Files:                                  4520
  Cleanups:                                  0
  Hits:                                      5
  Misses:                                   13
  Reads:                                    18
  Writes:                                   13
Remote storage:
  Hits:                                      3
  Misses:                                   10
  Reads:                                    13
  Writes:                                    3
  Errors:                                    1
  Timeouts:                                  0
`

func Test_ParseCcacheStats(t *testing.T) {
	t.Run("parses cacheable call counts", func(t *testing.T) {
		stats, err := ParseCcacheStats([]byte(sampleStatsOutput))

		assert.NoError(t, err)
		assert.Equal(t, 3, stats.DirectCacheHit)
		assert.Equal(t, 2, stats.PreprocessedCacheHit)
		assert.Equal(t, 13, stats.CacheMiss)
		assert.Equal(t, 18, stats.CacheableCalls)
	})

	t.Run("parses uncacheable call counts", func(t *testing.T) {
		stats, err := ParseCcacheStats([]byte(sampleStatsOutput))

		assert.NoError(t, err)
		assert.Equal(t, 2, stats.UncacheableCalls)
		assert.Equal(t, 2, stats.CalledForLink)
		assert.Equal(t, 0, stats.CalledForPreprocessing)
	})

	t.Run("derives total calls", func(t *testing.T) {
		stats, err := ParseCcacheStats([]byte(sampleStatsOutput))

		assert.NoError(t, err)
		assert.Equal(t, 20, stats.TotalCalls) // 18 cacheable + 2 uncacheable
	})

	t.Run("computes cache hit rate", func(t *testing.T) {
		// 3 direct + 2 preprocessed = 5 hits out of 18 total cacheable calls → 5/18
		stats, err := ParseCcacheStats([]byte(sampleStatsOutput))

		assert.NoError(t, err)
		assert.InDelta(t, 5.0/18.0, stats.CacheHitRate, 1e-9)
	})

	t.Run("parses local storage fields", func(t *testing.T) {
		stats, err := ParseCcacheStats([]byte(sampleStatsOutput))

		assert.NoError(t, err)
		assert.InDelta(t, 0.4, stats.CacheSizeGiB, 1e-9)
		assert.InDelta(t, 10.0, stats.MaxCacheSizeGiB, 1e-9)
		assert.Equal(t, 4520, stats.FilesInCache)
		assert.Equal(t, 0, stats.CleanupsPerformed)
		assert.Equal(t, 5, stats.LocalStorageHit)
		assert.Equal(t, 13, stats.LocalStorageMiss)
		assert.Equal(t, 18, stats.LocalStorageReads)
		assert.Equal(t, 13, stats.LocalStorageWrite)
	})

	t.Run("parses remote storage fields", func(t *testing.T) {
		stats, err := ParseCcacheStats([]byte(sampleStatsOutput))

		assert.NoError(t, err)
		assert.Equal(t, 3, stats.RemoteStorageHit)
		assert.Equal(t, 10, stats.RemoteStorageMiss)
		assert.Equal(t, 13, stats.RemoteStorageReads)
		assert.Equal(t, 3, stats.RemoteStorageWrite)
		assert.Equal(t, 1, stats.RemoteStorageError)
		assert.Equal(t, 0, stats.RemoteStorageTimeout)
	})

	t.Run("hit rate is zero when no cacheable calls", func(t *testing.T) {
		stats, err := ParseCcacheStats([]byte(`Cacheable calls:                             0
  Hits:                                      0
    Direct:                                  0
    Preprocessed:                            0
  Misses:                                    0
`))

		assert.NoError(t, err)
		assert.Equal(t, 0.0, stats.CacheHitRate)
	})

	t.Run("hit rate is 1.0 when all calls are direct hits", func(t *testing.T) {
		stats, err := ParseCcacheStats([]byte(`Cacheable calls:                             5
  Hits:                                      5 / 5  (100.00 %)
    Direct:                                  5
    Preprocessed:                            0
  Misses:                                    0
`))

		assert.NoError(t, err)
		assert.Equal(t, 1.0, stats.CacheHitRate)
	})

	t.Run("returns no error on empty input", func(t *testing.T) {
		stats, err := ParseCcacheStats([]byte(""))

		assert.NoError(t, err)
		assert.Equal(t, CcacheStats{}, stats)
	})

	t.Run("unknown lines are silently ignored", func(t *testing.T) {
		_, err := ParseCcacheStats([]byte("Future field: 99\n"))

		assert.NoError(t, err)
	})
}
