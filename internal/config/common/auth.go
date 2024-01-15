package cacheconfigcommon

import "errors"

var (
	errAuthTokenNotProvided   = errors.New("AuthToken not provided")
	errWorkspaceIDNotProvided = errors.New("WorkspaceID not provided")
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
// - if BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN is provided, use that
// - otherwise, if BITRISE_BUILD_CACHE_AUTH_TOKEN and BITRISE_BUILD_CACHE_WORKSPACE_ID is provided, use that
// - otherwise return error
func ReadAuthConfigFromEnvironments(envProvider func(string) string) (CacheAuthConfig, error) {
	if serviceToken := envProvider("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN"); len(serviceToken) > 0 {
		// Bitrise service access token specified, use it for auth.
		// It's a JWT token which already includes the workspace ID.
		return CacheAuthConfig{
			AuthToken: serviceToken,
		}, nil
	}

	// No Bitrise Service Access Token specified.
	// In this case both AuthToken and Workspace ID required,
	authTokenEnv := envProvider("BITRISE_BUILD_CACHE_AUTH_TOKEN")
	if len(authTokenEnv) < 1 {
		return CacheAuthConfig{}, errAuthTokenNotProvided
	}

	workspaceIDEnv := envProvider("BITRISE_BUILD_CACHE_WORKSPACE_ID")
	if len(workspaceIDEnv) < 1 {
		return CacheAuthConfig{}, errWorkspaceIDNotProvided
	}

	return CacheAuthConfig{
		AuthToken:   authTokenEnv,
		WorkspaceID: workspaceIDEnv,
	}, nil
}
