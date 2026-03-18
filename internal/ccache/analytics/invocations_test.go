//go:build unit

package analytics

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ParseCcacheStats(t *testing.T) {
	t.Run("parses flat JSON from ccache output", func(t *testing.T) {
		data := []byte(`{
			"direct_cache_hit": 3,
			"preprocessed_cache_hit": 1,
			"cache_miss": 2
		}`)

		stats, err := ParseCcacheStats(data)

		require.NoError(t, err)
		assert.Equal(t, 3, stats.DirectCacheHit)
		assert.Equal(t, 1, stats.PreprocessedCacheHit)
		assert.Equal(t, 2, stats.CacheMiss)
	})

	t.Run("computes cache hit rate from hits and misses", func(t *testing.T) {
		// 3 direct + 1 preprocessed = 4 hits out of 6 total → 0.666...
		data := []byte(`{"direct_cache_hit": 3, "preprocessed_cache_hit": 1, "cache_miss": 2}`)

		stats, err := ParseCcacheStats(data)

		require.NoError(t, err)
		assert.InDelta(t, 4.0/6.0, stats.CacheHitRate, 1e-9)
	})

	t.Run("hit rate is zero when there are no attempts", func(t *testing.T) {
		data := []byte(`{"direct_cache_hit": 0, "preprocessed_cache_hit": 0, "cache_miss": 0}`)

		stats, err := ParseCcacheStats(data)

		require.NoError(t, err)
		assert.Equal(t, 0.0, stats.CacheHitRate)
	})

	t.Run("hit rate is 1.0 when all attempts are hits", func(t *testing.T) {
		data := []byte(`{"direct_cache_hit": 5, "preprocessed_cache_hit": 0, "cache_miss": 0}`)

		stats, err := ParseCcacheStats(data)

		require.NoError(t, err)
		assert.Equal(t, 1.0, stats.CacheHitRate)
	})

	t.Run("hit rate is 0.0 when all attempts are misses", func(t *testing.T) {
		data := []byte(`{"direct_cache_hit": 0, "preprocessed_cache_hit": 0, "cache_miss": 10}`)

		stats, err := ParseCcacheStats(data)

		require.NoError(t, err)
		assert.Equal(t, 0.0, stats.CacheHitRate)
	})

	t.Run("unknown fields are ignored", func(t *testing.T) {
		data := []byte(`{"direct_cache_hit": 1, "cache_miss": 1, "future_field": 99}`)

		stats, err := ParseCcacheStats(data)

		require.NoError(t, err)
		assert.Equal(t, 1, stats.DirectCacheHit)
	})

	t.Run("returns error on malformed JSON", func(t *testing.T) {
		_, err := ParseCcacheStats([]byte(`not json`))

		assert.Error(t, err)
	})
}
