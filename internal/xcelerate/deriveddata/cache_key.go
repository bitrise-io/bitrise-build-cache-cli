package deriveddata

import (
	"fmt"
	"runtime"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

type CacheKeyParams struct {
	IsFallback bool
}

func GetCacheKey(envs map[string]string, keyParams CacheKeyParams) (string, error) {
	branch := envs["BITRISE_GIT_BRANCH"]
	if branch == "" && !keyParams.IsFallback {
		return "", fmt.Errorf("cache key is required if BITRISE_GIT_BRANCH env var is not set")
	}
	// Branches can contain '/', which would split the kv/<key> resource name
	// into extra segments and collapse the per-OS keys onto one another.
	branch = common.SanitizeCacheKeyComponent(branch)

	appSlug := envs["BITRISE_APP_SLUG"]
	if appSlug == "" {
		return "", fmt.Errorf("cache key is required if BITRISE_APP_SLUG env var is not set")
	}

	os := runtime.GOOS

	if keyParams.IsFallback {
		return fmt.Sprintf("xcode-cache-metadata-%s-%s", appSlug, os), nil
	}

	return fmt.Sprintf("xcode-cache-metadata-%s-%s-%s", appSlug, branch, os), nil
}
