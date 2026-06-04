//go:build unit

package deriveddata

import (
	"runtime"
	"strings"
	"testing"
)

func TestGetCacheKeySanitizesBranchSlash(t *testing.T) {
	envs := map[string]string{
		"BITRISE_APP_SLUG":   "app-slug",
		"BITRISE_GIT_BRANCH": "renovate/all-non-major-updates",
	}

	key, err := GetCacheKey(envs, CacheKeyParams{})
	if err != nil {
		t.Fatalf("GetCacheKey returned error: %v", err)
	}

	if strings.Contains(key, "/") {
		t.Errorf("cache key must not contain '/', got %q", key)
	}
	want := "xcode-cache-metadata-app-slug-renovate_all-non-major-updates-" + runtime.GOOS
	if key != want {
		t.Errorf("GetCacheKey = %q, want %q", key, want)
	}
}

func TestGetCacheKeyFallbackHasNoBranch(t *testing.T) {
	envs := map[string]string{
		"BITRISE_APP_SLUG":   "app-slug",
		"BITRISE_GIT_BRANCH": "renovate/all-non-major-updates",
	}

	key, err := GetCacheKey(envs, CacheKeyParams{IsFallback: true})
	if err != nil {
		t.Fatalf("GetCacheKey returned error: %v", err)
	}

	want := "xcode-cache-metadata-app-slug-" + runtime.GOOS
	if key != want {
		t.Errorf("fallback GetCacheKey = %q, want %q", key, want)
	}
}
