package common

import "errors"

var (
	ErrAuthTokenNotProvided   = errors.New("BITRISE_BUILD_CACHE_AUTH_TOKEN or BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN environment variable not set")
	ErrWorkspaceIDNotProvided = errors.New("BITRISE_BUILD_CACHE_WORKSPACE_ID environment variable not set")
)

// CacheAuthConfig holds the auth config for the cache.
type CacheAuthConfig struct {
	AuthToken   string
	WorkspaceID string
}

// TokenInGradleFormat returns the auth token in gradle format.
func (cac CacheAuthConfig) TokenInGradleFormat() string {
	if cac.WorkspaceID == "" {
		return cac.AuthToken
	}

	return cac.WorkspaceID + ":" + cac.AuthToken
}

// ReadAuthConfigFromEnvironments reads auth information from the environment variables
func ReadAuthConfigFromEnvironments(envs map[string]string) (CacheAuthConfig, error) {
	authTokenEnv := envs["BITRISE_BUILD_CACHE_AUTH_TOKEN"]
	workspaceIDEnv := envs["BITRISE_BUILD_CACHE_WORKSPACE_ID"]

	if len(authTokenEnv) > 0 && len(workspaceIDEnv) > 0 {
		return CacheAuthConfig{
			AuthToken:   authTokenEnv,
			WorkspaceID: workspaceIDEnv,
		}, nil
	}

	// Try to fall back to JWT which is always available on Bitrise.
	// It's a JWT token which already includes the workspace ID.
	if serviceToken := envs["BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN"]; len(serviceToken) > 0 {
		return CacheAuthConfig{
			AuthToken: serviceToken,
		}, nil
	}

	// Write specific errors for each case.
	if len(authTokenEnv) < 1 {
		return CacheAuthConfig{}, ErrAuthTokenNotProvided
	}

	return CacheAuthConfig{}, ErrWorkspaceIDNotProvided
}
