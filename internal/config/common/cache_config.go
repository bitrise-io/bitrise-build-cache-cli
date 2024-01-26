package common

type CacheConfigMetadata struct {
	CIProvider string
	RepoURL    string
	// BitriseCI specific
	BitriseAppID        string
	BitriseStepID       string
	BitriseWorkflowName string
	BitriseBuildID      string
}

const (
	// CIProviderBitrise ...
	CIProviderBitrise = "bitrise"
	// CIProviderCircleCI ...
	CIProviderCircleCI = "circle-ci"
	// CIProviderGitHubActions ...
	CIProviderGitHubActions = "github-actions"

	// --- not used yet ---
	// CIProviderJenkins ...
	// CIProviderJenkins = "jenkins"
	// CIProviderUnknown ...
	// CIProviderUnknown = "other"
)

// EnvProviderFunc is a function which returns the value of an environment variable.
// It's compatible with os.Getenv - os.Getenv can be passed as an EnvProviderFunc.
type EnvProviderFunc func(string) string

func detectCIProvider(envProvider EnvProviderFunc) string {
	if envProvider("BITRISE_IO") != "" {
		// https://devcenter.bitrise.io/en/references/available-environment-variables.html
		return CIProviderBitrise
	}
	if envProvider("CIRCLECI") != "" {
		// https://circleci.com/docs/variables/#built-in-environment-variables
		return CIProviderCircleCI
	}
	if envProvider("GITHUB_ACTIONS") != "" {
		// https://docs.github.com/en/actions/learn-github-actions/variables#default-environment-variables
		return CIProviderGitHubActions
	}

	return ""
}

func createCacheConfigMetadata(provider, repoURL string,
	bitriseAppID, bitriseStepID, bitriseWorkflowName, bitriseBuildID string,
) CacheConfigMetadata {
	return CacheConfigMetadata{
		CIProvider: provider,
		RepoURL:    repoURL,
		// BitriseCI specific
		BitriseAppID:        bitriseAppID,
		BitriseStepID:       bitriseStepID,
		BitriseWorkflowName: bitriseWorkflowName,
		BitriseBuildID:      bitriseBuildID,
	}
}

// NewCacheConfigMetadata creates a new CacheConfigMetadata instance based on the environment variables.
func NewCacheConfigMetadata(envProvider EnvProviderFunc) CacheConfigMetadata {
	provider := detectCIProvider(envProvider)
	switch provider {
	case CIProviderBitrise:
		return createCacheConfigMetadata(provider, envProvider("GIT_REPOSITORY_URL"),
			// Bitrise CI specific
			envProvider("BITRISE_APP_SLUG"), envProvider("BITRISE_STEP_EXECUTION_ID"),
			envProvider("BITRISE_TRIGGERED_WORKFLOW_TITLE"), envProvider("BITRISE_BUILD_SLUG"))
	case CIProviderCircleCI:
		return createCacheConfigMetadata(provider, envProvider("CIRCLE_REPOSITORY_URL"),
			"", "", "", "")
	case CIProviderGitHubActions:
		repoURL := envProvider("GITHUB_SERVER_URL") + "/" + envProvider("GITHUB_REPOSITORY")

		return createCacheConfigMetadata(provider, repoURL,
			"", "", "", "")
	}

	return CacheConfigMetadata{}
}
