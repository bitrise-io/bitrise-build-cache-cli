//go:build unit

package deriveddata

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetCacheKeySanitizesBranchSlash(t *testing.T) {
	envs := map[string]string{
		"BITRISE_APP_SLUG":   "app-slug",
		"BITRISE_GIT_BRANCH": "renovate/all-non-major-updates",
	}

	key, err := GetCacheKey(envs, CacheKeyParams{})

	assert.NoError(t, err)
	assert.NotContains(t, key, "/", "cache key must not contain '/'")
	assert.Equal(t, "xcode-cache-metadata-app-slug-renovate_all-non-major-updates-"+runtime.GOOS, key)
}

func TestGetCacheKeyFallbackHasNoBranch(t *testing.T) {
	envs := map[string]string{
		"BITRISE_APP_SLUG":   "app-slug",
		"BITRISE_GIT_BRANCH": "renovate/all-non-major-updates",
	}

	key, err := GetCacheKey(envs, CacheKeyParams{IsFallback: true})

	assert.NoError(t, err)
	assert.Equal(t, "xcode-cache-metadata-app-slug-"+runtime.GOOS, key)
}
