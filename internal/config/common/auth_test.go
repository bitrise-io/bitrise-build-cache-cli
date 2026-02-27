package common

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeJWT(payload map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))

	payloadJSON, err := json.Marshal(payload) //nolint:errchkjson
	if err != nil {
		panic(err)
	}

	body := base64.RawURLEncoding.EncodeToString(payloadJSON)
	sig := base64.RawURLEncoding.EncodeToString([]byte("signature"))

	return header + "." + body + "." + sig
}

func TestReadAuthConfigFromEnvironments(t *testing.T) {
	validJWT := makeJWT(map[string]any{
		"default": map[string]any{
			"org_id": []string{"jwt-workspace-id"},
			"app_id": []string{"jwt-app-id"},
		},
	})

	tests := []struct {
		name           string
		envVars        map[string]string
		expectedError  string
		expectedConfig CacheAuthConfig
	}{
		{
			name:          "No envs specified",
			envVars:       map[string]string{},
			expectedError: ErrAuthTokenNotProvided.Error(),
		},
		{
			name: "Only BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN set with valid JWT",
			envVars: map[string]string{
				"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": validJWT,
			},
			expectedConfig: CacheAuthConfig{
				AuthToken:   validJWT,
				WorkspaceID: "jwt-workspace-id",
				IsJWT:       true,
			},
		},
		{
			name: "Both BITRISE_BUILD_CACHE_AUTH_TOKEN and BITRISE_BUILD_CACHE_WORKSPACE_ID set",
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "auth-token",
				"BITRISE_BUILD_CACHE_WORKSPACE_ID": "workspace-id",
			},
			expectedConfig: CacheAuthConfig{
				AuthToken:   "auth-token",
				WorkspaceID: "workspace-id",
			},
		},
		{
			name: "Only BITRISE_BUILD_CACHE_AUTH_TOKEN set",
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_AUTH_TOKEN": "auth-token",
			},
			expectedError: ErrWorkspaceIDNotProvided.Error(),
		},
		{
			name: "Only BITRISE_BUILD_CACHE_WORKSPACE_ID set",
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_WORKSPACE_ID": "workspace-id",
			},
			expectedError: ErrAuthTokenNotProvided.Error(),
		},
		{
			name: "JWT with missing org_id claim",
			envVars: map[string]string{
				"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": makeJWT(map[string]any{
					"default": map[string]any{
						"app_id": []string{"app-id"},
					},
				}),
			},
			expectedError: "extract workspace ID from JWT: org_id claim is missing from JWT",
		},
		{
			name: "JWT with empty org_id claim",
			envVars: map[string]string{
				"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": makeJWT(map[string]any{
					"default": map[string]any{
						"org_id": []string{""},
					},
				}),
			},
			expectedError: "extract workspace ID from JWT: org_id claim is empty in JWT",
		},
		{
			name: "Invalid JWT format",
			envVars: map[string]string{
				"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": "not-a-jwt",
			},
			expectedError: "extract workspace ID from JWT: invalid JWT format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := ReadAuthConfigFromEnvironments(tt.envVars)
			if tt.expectedError != "" {
				require.EqualError(t, err, tt.expectedError)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedConfig, config)
			}
		})
	}
}

func TestTokenInGradleFormat(t *testing.T) {
	tests := []struct {
		name   string
		config CacheAuthConfig
		want   string
	}{
		{
			name: "PAT with workspace ID",
			config: CacheAuthConfig{
				AuthToken:   "auth-token",
				WorkspaceID: "workspace-id",
			},
			want: "workspace-id:auth-token",
		},
		{
			name: "JWT returns token as-is",
			config: CacheAuthConfig{
				AuthToken:   "jwt-token",
				WorkspaceID: "workspace-id",
				IsJWT:       true,
			},
			want: "jwt-token",
		},
		{
			name: "No workspace ID returns token as-is",
			config: CacheAuthConfig{
				AuthToken: "auth-token",
			},
			want: "auth-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.config.TokenInGradleFormat())
		})
	}
}

func TestExtractWorkspaceIDFromJWT(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		want    string
		wantErr string
	}{
		{
			name:  "valid JWT with org_id",
			token: makeJWT(map[string]any{"default": map[string]any{"org_id": []string{"my-org"}}}),
			want:  "my-org",
		},
		{
			name:    "invalid format - not enough parts",
			token:   "only.two",
			wantErr: "invalid JWT format",
		},
		{
			name:    "invalid base64 payload",
			token:   "header.!!!invalid!!!.sig",
			wantErr: "decode JWT payload",
		},
		{
			name:    "invalid JSON payload",
			token:   "header." + base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".sig",
			wantErr: "parse JWT payload",
		},
		{
			name:    "missing org_id claim",
			token:   makeJWT(map[string]any{"default": map[string]any{}}),
			wantErr: "org_id claim is missing from JWT",
		},
		{
			name:    "empty org_id",
			token:   makeJWT(map[string]any{"default": map[string]any{"org_id": []string{""}}}),
			wantErr: "org_id claim is empty in JWT",
		},
		{
			name:  "multiple org_ids uses first",
			token: makeJWT(map[string]any{"default": map[string]any{"org_id": []string{"first", "second"}}}),
			want:  "first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractWorkspaceIDFromJWT(tt.token)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
