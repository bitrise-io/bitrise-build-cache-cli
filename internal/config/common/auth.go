package common

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrAuthTokenNotProvided   = errors.New("BITRISE_BUILD_CACHE_AUTH_TOKEN or BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN environment variable not set")
	ErrWorkspaceIDNotProvided = errors.New("BITRISE_BUILD_CACHE_WORKSPACE_ID environment variable not set")
)

// CacheAuthConfig holds the auth config for the cache.
type CacheAuthConfig struct {
	AuthToken   string
	WorkspaceID string
	IsJWT       bool
}

// TokenInGradleFormat returns the auth token in gradle format.
// For JWT tokens, the token is sent as-is (the workspace ID is embedded in the JWT).
// For PAT tokens, the format is "workspaceID:token".
func (cac CacheAuthConfig) TokenInGradleFormat() string {
	if cac.IsJWT || cac.WorkspaceID == "" {
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
		workspaceID, err := extractWorkspaceIDFromJWT(serviceToken)
		if err != nil {
			return CacheAuthConfig{}, fmt.Errorf("extract workspace ID from JWT: %w", err)
		}

		return CacheAuthConfig{
			AuthToken:   serviceToken,
			WorkspaceID: workspaceID,
			IsJWT:       true,
		}, nil
	}

	// Write specific errors for each case.
	if len(authTokenEnv) < 1 {
		return CacheAuthConfig{}, ErrAuthTokenNotProvided
	}

	return CacheAuthConfig{}, ErrWorkspaceIDNotProvided
}

// jwtDefaultClaims represents the "default" claim in the Bitrise JWT payload.
type jwtDefaultClaims struct {
	OrgID []string `json:"org_id"`
}

// jwtPayload represents the JWT payload structure with the "default" claim.
type jwtPayload struct {
	Default jwtDefaultClaims `json:"default"`
}

// extractWorkspaceIDFromJWT extracts the workspace ID (org_id) from a JWT token
// without validating the token signature.
func extractWorkspaceIDFromJWT(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 { //nolint:mnd
		return "", errors.New("invalid JWT format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode JWT payload: %w", err)
	}

	var claims jwtPayload
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("parse JWT payload: %w", err)
	}

	if len(claims.Default.OrgID) == 0 {
		return "", errors.New("org_id claim is missing from JWT")
	}

	workspaceID := claims.Default.OrgID[0]
	if workspaceID == "" {
		return "", errors.New("org_id claim is empty in JWT")
	}

	return workspaceID, nil
}
