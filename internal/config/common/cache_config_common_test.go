package cacheconfigcommon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createEnvProvider(envs map[string]string) func(string) string {
	return func(s string) string { return envs[s] }
}

func Test_ReadAuthConfigFromEnvironments(t *testing.T) {
	t.Run("No envs provided", func(t *testing.T) {
		authToken, err := ReadAuthConfigFromEnvironments(createEnvProvider(map[string]string{}))
		require.EqualError(t, err, "AuthToken not provided")
		assert.Equal(t, CacheAuthConfig{}, authToken)
	})

	t.Run("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN", func(t *testing.T) {
		authToken, err := ReadAuthConfigFromEnvironments(createEnvProvider(map[string]string{
			"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": "ServiceAccessTokenValue",
		}))
		require.NoError(t, err)
		assert.Equal(t, CacheAuthConfig{AuthToken: "ServiceAccessTokenValue"}, authToken)
	})

	t.Run("BITRISE_BUILD_CACHE_AUTH_TOKEN", func(t *testing.T) {
		authToken, err := ReadAuthConfigFromEnvironments(createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN": "BuildCacheAuthTokenValue",
		}))
		require.EqualError(t, err, "WorkspaceID not provided")
		assert.Equal(t, CacheAuthConfig{}, authToken)
	})

	t.Run("BITRISE_BUILD_CACHE_AUTH_TOKEN & BITRISE_BUILD_CACHE_WORKSPACE_ID", func(t *testing.T) {
		authToken, err := ReadAuthConfigFromEnvironments(createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		}))
		require.NoError(t, err)
		assert.Equal(t, CacheAuthConfig{
			AuthToken:   "AuthTokenValue",
			WorkspaceID: "WorkspaceIDValue",
		}, authToken)
	})

	t.Run("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN & BITRISE_BUILD_CACHE_AUTH_TOKEN & BITRISE_BUILD_CACHE_WORKSPACE_ID", func(t *testing.T) {
		authToken, err := ReadAuthConfigFromEnvironments(createEnvProvider(map[string]string{
			"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": "ServiceAccessTokenValue",
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":          "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID":        "WorkspaceIDValue",
		}))
		require.NoError(t, err)
		// BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN wins
		assert.Equal(t, CacheAuthConfig{AuthToken: "ServiceAccessTokenValue"}, authToken)
	})
}

func TestCacheAuthConfig_TokenInGradleFormat(t *testing.T) {
	type fields struct {
		AuthToken   string
		WorkspaceID string
	}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{
			name: "No WorkspaceID",
			fields: fields{
				AuthToken: "AuthTokenValue",
			},
			want: "AuthTokenValue",
		},
		{
			name: "With WorkspaceID",
			fields: fields{
				AuthToken:   "AuthTokenValue",
				WorkspaceID: "WorkspaceIDValue",
			},
			want: "WorkspaceIDValue:AuthTokenValue",
		},
	}
	for _, tt := range tests { //nolint:varnamelen
		t.Run(tt.name, func(t *testing.T) {
			cac := CacheAuthConfig{
				AuthToken:   tt.fields.AuthToken,
				WorkspaceID: tt.fields.WorkspaceID,
			}
			if got := cac.TokenInGradleFormat(); got != tt.want {
				t.Errorf("CacheAuthConfig.TokenInGradleFormat() = %v, want %v", got, tt.want)
			}
		})
	}
}
