package common

type CacheConfigMetadata struct {
	CIProvider string
	RepoURL    string
	// BitriseCI specific
	BitriseAppID           string
	BitriseWorkflowName    string
	BitriseBuildID         string
	BitriseStepExecutionID string
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

type bitriseCISpecificMetadata struct {
	BitriseAppID           string
	BitriseWorkflowName    string
	BitriseBuildID         string
	BitriseStepExecutionID string
}

func createCacheConfigMetadata(provider, repoURL string,
	bitriseCIMetadata bitriseCISpecificMetadata,
) CacheConfigMetadata {
	return CacheConfigMetadata{
		CIProvider: provider,
		RepoURL:    repoURL,
		// BitriseCI specific
		BitriseAppID:        bitriseCIMetadata.BitriseAppID,
		BitriseWorkflowName: bitriseCIMetadata.BitriseWorkflowName,
		BitriseBuildID:      bitriseCIMetadata.BitriseBuildID,
	}
}

// NewCacheConfigMetadata creates a new CacheConfigMetadata instance based on the environment variables.
func NewCacheConfigMetadata(envProvider EnvProviderFunc) CacheConfigMetadata {
	provider := detectCIProvider(envProvider)
	switch provider {
	case CIProviderBitrise:
		return createCacheConfigMetadata(provider, envProvider("GIT_REPOSITORY_URL"),
			bitriseCISpecificMetadata{
				BitriseAppID:           envProvider("BITRISE_APP_SLUG"),
				BitriseWorkflowName:    envProvider("BITRISE_TRIGGERED_WORKFLOW_TITLE"),
				BitriseBuildID:         envProvider("BITRISE_BUILD_SLUG"),
				BitriseStepExecutionID: envProvider("BITRISE_STEP_EXECUTION_ID"),
			})
	case CIProviderCircleCI:
		return createCacheConfigMetadata(provider, envProvider("CIRCLE_REPOSITORY_URL"),
			bitriseCISpecificMetadata{})
	case CIProviderGitHubActions:
		repoURL := envProvider("GITHUB_SERVER_URL") + "/" + envProvider("GITHUB_REPOSITORY")

		return createCacheConfigMetadata(provider, repoURL,
			bitriseCISpecificMetadata{})
	}

	return CacheConfigMetadata{}
}
