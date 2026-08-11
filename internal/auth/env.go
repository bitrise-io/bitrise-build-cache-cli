package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	EnvAuthToken   = "BITRISE_BUILD_CACHE_AUTH_TOKEN"   //nolint:gosec // env-var key, not a credential
	EnvWorkspaceID = "BITRISE_BUILD_CACHE_WORKSPACE_ID" //nolint:gosec // env-var key, not a credential
	EnvJWT         = "BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN"
	EnvUsername    = "BITRISE_BUILD_CACHE_USERNAME"
)

var (
	ErrTokenNotProvided       = errors.New(EnvAuthToken + " or " + EnvJWT + " environment variable not set")
	ErrWorkspaceIDNotProvided = errors.New(EnvWorkspaceID + " environment variable not set")
	ErrWorkspaceNotSelected   = errors.New("signed in, but no workspace is selected yet — run `bitrise-build-cache auth workspace --list` to see them, then `auth workspace --set <slug>`")
)

type jwtPermissionClaims struct {
	OrgID []string `json:"org_id"`
}

type jwtPermission struct {
	Rsname string              `json:"rsname"`
	Claims jwtPermissionClaims `json:"claims"`
}

type jwtAuthorization struct {
	Permissions []jwtPermission `json:"permissions"`
}

type jwtPayload struct {
	Authorization jwtAuthorization `json:"authorization"`
}

// ParseJWTWorkspaceID extracts org_id from a Bitrise UMA-style JWT without
// verifying the signature (we trust the issuer — Bitrise mints these per build).
func ParseJWTWorkspaceID(token string) (string, error) {
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

	for _, perm := range claims.Authorization.Permissions {
		if perm.Rsname != "default" {
			continue
		}

		if len(perm.Claims.OrgID) == 0 {
			return "", errors.New("org_id claim is missing from JWT")
		}

		if perm.Claims.OrgID[0] == "" {
			return "", errors.New("org_id claim is empty in JWT")
		}

		return perm.Claims.OrgID[0], nil
	}

	return "", errors.New("'default' permission not found in JWT")
}
