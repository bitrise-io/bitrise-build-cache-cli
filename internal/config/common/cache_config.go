package common

type CacheConfig struct {
	CIProvider string
	RepoURL    string
}

const (
	// CIProviderBitrise ...
	CIProviderBitrise = "bitrise"
	// CIProviderCircleCI ...
	CIProviderCircleCI = "circle-ci"
	// CIProviderGitHubActions ...
	CIProviderGitHubActions = "github-actions"
)

// EnvProviderFunc is a function which returns the value of an environment variable.
// It's compatible with os.Getenv - os.Getenv can be passed as an EnvProviderFunc.
type EnvProviderFunc func(string) string

func detectCIProvider(envProvider EnvProviderFunc) string {
	if envProvider("BITRISE_IO") != "" {
		return CIProviderBitrise
	}
	if envProvider("CIRCLECI") != "" {
		return CIProviderCircleCI
	}
	if envProvider("GITHUB_ACTIONS") != "" {
		return CIProviderGitHubActions
	}

	return ""
}

func createCacheConfig(provider, repoURL string) CacheConfig {
	return CacheConfig{
		CIProvider: provider,
		RepoURL:    repoURL,
	}
}

// NewCacheConfig creates a new CacheConfig instance based on the environment variables.
func NewCacheConfig(envProvider EnvProviderFunc) CacheConfig {
	provider := detectCIProvider(envProvider)
	switch provider {
	case CIProviderBitrise:
		return createCacheConfig(provider, envProvider("GIT_REPOSITORY_URL"))
	case CIProviderCircleCI:
		return createCacheConfig(provider, envProvider("CIRCLE_REPOSITORY_URL"))
	case CIProviderGitHubActions:
		repoURL := envProvider("GITHUB_SERVER_URL") + "/" + envProvider("GITHUB_REPOSITORY")

		return createCacheConfig(provider, repoURL)
	}

	return CacheConfig{}
}
