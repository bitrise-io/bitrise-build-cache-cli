package xcode

import "fmt"

type CacheKeyParams struct {
	IsFallback bool
}

func GetCacheKey(envProvider func(string) string, keyParams CacheKeyParams) (string, error) {
	branch := envProvider("BITRISE_GIT_BRANCH")
	if branch == "" && !keyParams.IsFallback {
		return "", fmt.Errorf("cache key is required if BITRISE_GIT_BRANCH env var is not set")
	}

	appSlug := envProvider("BITRISE_APP_SLUG")
	if appSlug == "" {
		return "", fmt.Errorf("cache key is required if BITRISE_APP_SLUG env var is not set")
	}

	if keyParams.IsFallback {
		return fmt.Sprintf("xcode-cache-metadata-%s", appSlug), nil
	}

	return fmt.Sprintf("xcode-cache-metadata-%s-%s", appSlug, branch), nil
}
