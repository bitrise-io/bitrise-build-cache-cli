package common

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestUMAJWT(orgID string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))

	payloadJSON, err := json.Marshal(map[string]any{ //nolint:errchkjson
		"authorization": map[string]any{
			"permissions": []map[string]any{
				{
					"rsname": "default",
					"claims": map[string]any{
						"org_id": []string{orgID},
						"app_id": []string{"test-app"},
					},
				},
			},
		},
	})
	if err != nil {
		panic(err)
	}

	body := base64.RawURLEncoding.EncodeToString(payloadJSON)
	sig := base64.RawURLEncoding.EncodeToString([]byte("signature"))

	return header + "." + body + "." + sig
}

func Test_ReadAuthConfigFromEnvironments(t *testing.T) {
	serviceJWT := makeTestUMAJWT("jwt-org-id")

	t.Run("No envs provided", func(t *testing.T) {
		authToken, err := ReadAuthConfigFromEnvironments(map[string]string{})
		require.EqualError(t, err, "BITRISE_BUILD_CACHE_AUTH_TOKEN or BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN environment variable not set")
		assert.Equal(t, CacheAuthConfig{}, authToken)
	})

	t.Run("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN", func(t *testing.T) {
		authToken, err := ReadAuthConfigFromEnvironments(map[string]string{
			"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": serviceJWT,
		})
		require.NoError(t, err)
		assert.Equal(t, CacheAuthConfig{
			AuthToken:   serviceJWT,
			WorkspaceID: "jwt-org-id",
			IsJWT:       true,
		}, authToken)
	})

	t.Run("BITRISE_BUILD_CACHE_AUTH_TOKEN", func(t *testing.T) {
		authToken, err := ReadAuthConfigFromEnvironments(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN": "BuildCacheAuthTokenValue",
		})
		require.EqualError(t, err, "BITRISE_BUILD_CACHE_WORKSPACE_ID environment variable not set")
		assert.Equal(t, CacheAuthConfig{}, authToken)
	})

	t.Run("BITRISE_BUILD_CACHE_AUTH_TOKEN & BITRISE_BUILD_CACHE_WORKSPACE_ID", func(t *testing.T) {
		authToken, err := ReadAuthConfigFromEnvironments(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})
		require.NoError(t, err)
		assert.Equal(t, CacheAuthConfig{
			AuthToken:   "AuthTokenValue",
			WorkspaceID: "WorkspaceIDValue",
		}, authToken)
	})

	t.Run("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN & BITRISE_BUILD_CACHE_AUTH_TOKEN & BITRISE_BUILD_CACHE_WORKSPACE_ID", func(t *testing.T) {
		authToken, err := ReadAuthConfigFromEnvironments(map[string]string{
			"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": serviceJWT,
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":          "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID":        "WorkspaceIDValue",
		})
		require.NoError(t, err)
		// BITRISE_BUILD_CACHE_AUTH_TOKEN wins
		assert.Equal(t, CacheAuthConfig{AuthToken: "AuthTokenValue", WorkspaceID: "WorkspaceIDValue"}, authToken)
	})
}

func TestCacheAuthConfig_TokenInGradleFormat(t *testing.T) {
	tests := []struct {
		name   string
		config CacheAuthConfig
		want   string
	}{
		{
			name: "No WorkspaceID",
			config: CacheAuthConfig{
				AuthToken: "AuthTokenValue",
			},
			want: "AuthTokenValue",
		},
		{
			name: "With WorkspaceID (PAT)",
			config: CacheAuthConfig{
				AuthToken:   "AuthTokenValue",
				WorkspaceID: "WorkspaceIDValue",
			},
			want: "WorkspaceIDValue:AuthTokenValue",
		},
		{
			name: "JWT returns token as-is even with WorkspaceID",
			config: CacheAuthConfig{
				AuthToken:   "jwt-token",
				WorkspaceID: "WorkspaceIDValue",
				IsJWT:       true,
			},
			want: "jwt-token",
		},
	}
	for _, tt := range tests { //nolint:varnamelen
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.TokenInGradleFormat(); got != tt.want {
				t.Errorf("CacheAuthConfig.TokenInGradleFormat() = %v, want %v", got, tt.want)
			}
		})
	}
}
