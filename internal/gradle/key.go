package gradle

import "fmt"

type CacheKeyParams struct {
	IsFallback bool
}

func (g *Cache) GetCacheKey(keyParams CacheKeyParams) (string, error) {
	branch := g.envProvider("BITRISE_GIT_BRANCH")
	if branch == "" && !keyParams.IsFallback {
		return "", fmt.Errorf("cache key is required if BITRISE_GIT_BRANCH env var is not set")
	}

	appSlug := g.envProvider("BITRISE_APP_SLUG")
	if appSlug == "" {
		return "", fmt.Errorf("cache key is required if BITRISE_APP_SLUG env var is not set")
	}

	if keyParams.IsFallback {
		return fmt.Sprintf("gradle-config-cache-metadata-%s", appSlug), nil
	}

	return fmt.Sprintf("gradle-config-cache-metadata-%s-%s", appSlug, branch), nil
}
