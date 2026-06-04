//go:build unit

package gradle

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetCacheKeySanitizesBranchSlash(t *testing.T) {
	c := NewCache(nil, map[string]string{
		"BITRISE_APP_SLUG":   "app-slug",
		"BITRISE_GIT_BRANCH": "renovate/all-non-major-updates",
	}, nil)

	key, err := c.GetCacheKey(CacheKeyParams{})

	assert.NoError(t, err)
	assert.NotContains(t, key, "/", "cache key must not contain '/'")
	assert.Equal(t, "gradle-config-cache-metadata-app-slug-renovate_all-non-major-updates-"+runtime.GOOS, key)
}

func TestGetCacheKeyFallbackHasNoBranch(t *testing.T) {
	c := NewCache(nil, map[string]string{
		"BITRISE_APP_SLUG":   "app-slug",
		"BITRISE_GIT_BRANCH": "renovate/all-non-major-updates",
	}, nil)

	key, err := c.GetCacheKey(CacheKeyParams{IsFallback: true})

	assert.NoError(t, err)
	assert.Equal(t, "gradle-config-cache-metadata-app-slug-"+runtime.GOOS, key)
}
