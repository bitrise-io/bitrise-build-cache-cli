package bazelconfig

import (
	"fmt"
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_ActivateBazelParams(t *testing.T) {
	prep := func() log.Logger {
		mockLogger := &mocks.Logger{}
		mockLogger.On("Infof", mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()

		return mockLogger
	}

	t.Run("TemplateInventory generates full configuration with default values", func(t *testing.T) {
		mockLogger := prep()
		params := DefaultActivateBazelParams()

		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
			"BITRISE_IO":                       "true",
			"BITRISE_APP_SLUG":                 "AppSlugValue",
			"BITRISE_TRIGGERED_WORKFLOW_TITLE": "WorkflowName1",
			"BITRISE_BUILD_SLUG":               "BuildID1",
		})

		// when
		inventory, err := params.TemplateInventory(mockLogger, envVars, true)

		// then
		require.NoError(t, err)

		// Check common configuration
		assert.Equal(t, "AuthTokenValue", inventory.Common.AuthToken)
		assert.Equal(t, "WorkspaceIDValue", inventory.Common.WorkspaceID)
		assert.True(t, inventory.Common.Debug)
		assert.Equal(t, "AppSlugValue", inventory.Common.AppSlug)
		assert.Equal(t, "bitrise", inventory.Common.CIProvider)
		assert.Equal(t, "WorkflowName1", inventory.Common.WorkflowName)
		assert.Equal(t, "BuildID1", inventory.Common.BuildID)
		assert.True(t, inventory.Common.Timestamps)

		// Check cache configuration (enabled by default)
		assert.True(t, inventory.Cache.Enabled)
		assert.False(t, inventory.Cache.IsPushEnabled)
		assert.NotEmpty(t, inventory.Cache.EndpointURLWithPort)

		// Check BES configuration (enabled by default)
		assert.True(t, inventory.BES.Enabled)
		assert.Equal(t, "grpcs://flare-bes.services.bitrise.io:443", inventory.BES.EndpointURLWithPort)

		// Check RBE configuration (disabled by default)
		assert.False(t, inventory.RBE.Enabled)
		assert.Empty(t, inventory.RBE.EndpointURLWithPort)
	})

	t.Run("TemplateInventory with auth error returns error", func(t *testing.T) {
		mockLogger := prep()
		params := DefaultActivateBazelParams()
		envVars := createEnvProvider(map[string]string{})

		// when
		_, err := params.TemplateInventory(mockLogger, envVars, false)

		// then
		require.EqualError(t, err, fmt.Errorf("read auth config from environment variables: %w", common.ErrAuthTokenNotProvided).Error())
	})

	t.Run("TemplateInventory with cache enabled and custom endpoint", func(t *testing.T) {
		mockLogger := prep()
		params := DefaultActivateBazelParams()
		params.Cache.Enabled = true
		params.Cache.PushEnabled = true
		params.Cache.Endpoint = "custom-endpoint:8080"

		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})

		// when
		inventory, err := params.TemplateInventory(mockLogger, envVars, false)

		// then
		require.NoError(t, err)
		assert.True(t, inventory.Cache.Enabled)
		assert.True(t, inventory.Cache.IsPushEnabled)
		assert.Equal(t, "custom-endpoint:8080", inventory.Cache.EndpointURLWithPort)
	})

	t.Run("TemplateInventory with cache disabled", func(t *testing.T) {
		mockLogger := prep()
		params := DefaultActivateBazelParams()
		params.Cache.Enabled = false

		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})

		// when
		inventory, err := params.TemplateInventory(mockLogger, envVars, false)

		// then
		require.NoError(t, err)
		assert.False(t, inventory.Cache.Enabled)
		assert.Empty(t, inventory.Cache.EndpointURLWithPort)
	})

	t.Run("TemplateInventory with BES disabled", func(t *testing.T) {
		mockLogger := prep()
		params := DefaultActivateBazelParams()
		params.BES.Enabled = false

		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})

		// when
		inventory, err := params.TemplateInventory(mockLogger, envVars, false)

		// then
		require.NoError(t, err)
		assert.False(t, inventory.BES.Enabled)
		assert.Empty(t, inventory.BES.EndpointURLWithPort)
	})

	t.Run("TemplateInventory with BES custom endpoint", func(t *testing.T) {
		mockLogger := prep()
		params := DefaultActivateBazelParams()
		params.BES.Enabled = true
		params.BES.Endpoint = "custom-bes:8080"

		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})

		// when
		inventory, err := params.TemplateInventory(mockLogger, envVars, false)

		// then
		require.NoError(t, err)
		assert.True(t, inventory.BES.Enabled)
		assert.Equal(t, "custom-bes:8080", inventory.BES.EndpointURLWithPort)
	})

	t.Run("TemplateInventory with RBE enabled but no endpoint", func(t *testing.T) {
		mockLogger := prep()
		params := DefaultActivateBazelParams()
		params.RBE.Enabled = true

		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})

		// when
		inventory, err := params.TemplateInventory(mockLogger, envVars, false)

		// then
		require.NoError(t, err)
		assert.False(t, inventory.RBE.Enabled)
		assert.Empty(t, inventory.RBE.EndpointURLWithPort)
	})

	t.Run("TemplateInventory with RBE enabled and custom endpoint", func(t *testing.T) {
		mockLogger := prep()
		params := DefaultActivateBazelParams()
		params.RBE.Enabled = true
		params.RBE.Endpoint = "custom-rbe:8080"

		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})

		// when
		inventory, err := params.TemplateInventory(mockLogger, envVars, false)

		// then
		require.NoError(t, err)
		assert.True(t, inventory.RBE.Enabled)
		assert.Equal(t, "custom-rbe:8080", inventory.RBE.EndpointURLWithPort)
	})

	t.Run("TemplateInventory with timestamps enabled", func(t *testing.T) {
		mockLogger := prep()
		params := DefaultActivateBazelParams()
		params.Timestamps = true

		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})

		// when
		inventory, err := params.TemplateInventory(mockLogger, envVars, false)

		// then
		require.NoError(t, err)
		assert.True(t, inventory.Common.Timestamps)
	})
}

func createEnvProvider(envs map[string]string) func(string) string {
	return func(key string) string {
		return envs[key]
	}
}
