package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadAuthConfigFromEnvironments(t *testing.T) {
	tests := []struct {
		name           string
		envVars        map[string]string
		expectedError  error
		expectedConfig CacheAuthConfig
	}{
		{
			name:          "No envs specified",
			envVars:       map[string]string{},
			expectedError: errAuthTokenNotProvided,
		},
		{
			name: "Only BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN set",
			envVars: map[string]string{
				"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": "service-token",
			},
			expectedError: nil,
			expectedConfig: CacheAuthConfig{
				AuthToken: "service-token",
			},
		},
		{
			name: "Both BITRISE_BUILD_CACHE_AUTH_TOKEN and BITRISE_BUILD_CACHE_WORKSPACE_ID set",
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "auth-token",
				"BITRISE_BUILD_CACHE_WORKSPACE_ID": "workspace-id",
			},
			expectedError: nil,
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
			expectedError: errWorkspaceIDNotProvided,
		},
		{
			name: "Only BITRISE_BUILD_CACHE_WORKSPACE_ID set",
			envVars: map[string]string{
				"BITRISE_BUILD_CACHE_WORKSPACE_ID": "workspace-id",
			},
			expectedError: errAuthTokenNotProvided,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envProvider := func(key string) string {
				return tt.envVars[key]
			}

			config, err := ReadAuthConfigFromEnvironments(envProvider)
			if tt.expectedError != nil {
				require.EqualError(t, err, tt.expectedError.Error())
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedConfig, config)
			}
		})
	}
}
