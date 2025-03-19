package common

import (
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createEnvProvider(envs map[string]string) func(string) string {
	return func(s string) string { return envs[s] }
}

func Test_ReadAuthConfigFromEnvironments(t *testing.T) {
	t.Run("No envs provided", func(t *testing.T) {
		authToken, err := ReadAuthConfigFromEnvironments(createEnvProvider(map[string]string{}))
		require.EqualError(t, err, "BITRISE_BUILD_CACHE_AUTH_TOKEN or BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN environment variable not set")
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
		require.EqualError(t, err, "BITRISE_BUILD_CACHE_WORKSPACE_ID environment variable not set")
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
		// BITRISE_BUILD_CACHE_AUTH_TOKEN wins
		assert.Equal(t, CacheAuthConfig{AuthToken: "AuthTokenValue", WorkspaceID: "WorkspaceIDValue"}, authToken)
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

func TestSelectCacheEndpointURL(t *testing.T) {
	tests := []struct {
		name                string
		providedEndpointURL string
		envVars             map[string]string
		expectedEndpointURL string
	}{
		{
			name:                "Explicit endpoint URL provided",
			providedEndpointURL: "grpcs://custom-endpoint.example.com",
			envVars:             map[string]string{},
			expectedEndpointURL: "grpcs://custom-endpoint.example.com",
		},
		{
			name:                "Env var endpoint URL used",
			providedEndpointURL: "",
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_ENDPOINT": "grpcs://env-var-endpoint.example.com",
			},
			expectedEndpointURL: "grpcs://env-var-endpoint.example.com",
		},
		{
			name:                "LAS1 datacenter",
			providedEndpointURL: "",
			envVars: map[string]string{
				"BITRISE_DEN_VM_DATACENTER": "LAS1",
			},
			expectedEndpointURL: consts.UnifiedCacheEndpointURL,
		},
		{
			name:                "ATL1 datacenter",
			providedEndpointURL: "",
			envVars: map[string]string{
				"BITRISE_DEN_VM_DATACENTER": "ATL1",
			},
			expectedEndpointURL: consts.UnifiedCacheEndpointURL,
		},
		{
			name:                "IAD1 datacenter",
			providedEndpointURL: "",
			envVars: map[string]string{
				"BITRISE_DEN_VM_DATACENTER": "IAD1",
			},
			expectedEndpointURL: consts.UnifiedCacheEndpointURL,
		},
		{
			name:                "ORD1 datacenter",
			providedEndpointURL: "",
			envVars: map[string]string{
				"BITRISE_DEN_VM_DATACENTER": "ORD1",
			},
			expectedEndpointURL: consts.UnifiedCacheEndpointURL,
		},
		{
			name:                "Default for unknown datacenter",
			providedEndpointURL: "",
			envVars: map[string]string{
				"BITRISE_DEN_VM_DATACENTER": "UNKNOWN",
			},
			expectedEndpointURL: consts.EndpointURLDefault,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envProvider := createEnvProvider(tt.envVars)
			result := SelectCacheEndpointURL(tt.providedEndpointURL, envProvider)
			assert.Equal(t, tt.expectedEndpointURL, result)
		})
	}
}

func TestSelectRBEEndpointURL(t *testing.T) {
	tests := []struct {
		name                string
		providedEndpointURL string
		envVars             map[string]string
		expectedEndpointURL string
	}{
		{
			name:                "Explicit endpoint URL provided",
			providedEndpointURL: "grpcs://custom-rbe-endpoint.example.com",
			envVars:             map[string]string{},
			expectedEndpointURL: "grpcs://custom-rbe-endpoint.example.com",
		},
		{
			name:                "Env var endpoint URL used",
			providedEndpointURL: "",
			envVars: map[string]string{
				"BITRISE_RBE_ENDPOINT": "grpcs://env-var-rbe-endpoint.example.com",
			},
			expectedEndpointURL: "grpcs://env-var-rbe-endpoint.example.com",
		},
		{
			name:                "IAD1 datacenter",
			providedEndpointURL: "",
			envVars: map[string]string{
				"BITRISE_DEN_VM_DATACENTER": "IAD1",
			},
			expectedEndpointURL: consts.UnifiedRBEEndpointURL,
		},
		{
			name:                "ORD1 datacenter",
			providedEndpointURL: "",
			envVars: map[string]string{
				"BITRISE_DEN_VM_DATACENTER": "ORD1",
			},
			expectedEndpointURL: consts.UnifiedRBEEndpointURL,
		},
		{
			name:                "Default for unsupported datacenter",
			providedEndpointURL: "",
			envVars: map[string]string{
				"BITRISE_DEN_VM_DATACENTER": "LAS1",
			},
			expectedEndpointURL: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envProvider := createEnvProvider(tt.envVars)
			result := SelectRBEEndpointURL(tt.providedEndpointURL, envProvider)
			assert.Equal(t, tt.expectedEndpointURL, result)
		})
	}
}