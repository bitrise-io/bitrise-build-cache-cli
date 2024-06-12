package xcode

import "fmt"

func GetCacheKey(envProvider func(string) string) (string, error) {
	branch := envProvider("BITRISE_GIT_BRANCH")
	if branch == "" {
		return "", fmt.Errorf("cache key is required if BITRISE_GIT_BRANCH is not set")
	}

	return fmt.Sprintf("xcode-deriveddata-%s", branch), nil
}
