package xcode

import "fmt"

func GetCacheKey(envProvider func(string) string, metadataKey bool) (string, error) {
	branch := envProvider("BITRISE_GIT_BRANCH")
	if branch == "" {
		return "", fmt.Errorf("cache key is required if BITRISE_GIT_BRANCH env var is not set")
	}

	appSlug := envProvider("BITRISE_APP_SLUG")
	if appSlug == "" {
		return "", fmt.Errorf("cache key is required if BITRISE_APP_SLUG env var is not set")
	}

	if metadataKey {
		return fmt.Sprintf("xcode-deriveddata-metadata-%s-%s", appSlug, branch), nil
	}

	return fmt.Sprintf("xcode-deriveddata-%s-%s", appSlug, branch), nil
}
