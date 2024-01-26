package cmd

import (
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func createEnvProvider(envs map[string]string) func(string) string {
	return func(s string) string { return envs[s] }
}

func Test_enableForGradleCmdFn(t *testing.T) {
	prep := func() (log.Logger, string) {
		mockLogger := &mocks.Logger{}
		mockLogger.On("Infof", mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
		tmpPath := t.TempDir()
		tmpGradleHomeDir := filepath.Join(tmpPath, ".gradle")

		return mockLogger, tmpGradleHomeDir
	}

	t.Run("No envs specified", func(t *testing.T) {
		mockLogger, tmpGradleHomeDir := prep()
		envVars := createEnvProvider(map[string]string{})
		err := enableForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

		// then
		require.EqualError(t, err, "read auth config from environment variables: AuthToken not provided")
	})

	t.Run("No envs specified", func(t *testing.T) {
		mockLogger, tmpGradleHomeDir := prep()
		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})

		// when
		err := enableForGradleCmdFn(mockLogger, tmpGradleHomeDir, envVars)

		// then
		require.NoError(t, err)
		//
		isInitFileExists, err := pathutil.NewPathChecker().IsPathExists(filepath.Join(tmpGradleHomeDir, "init.d", "bitrise-build-cache-init.gradle.kts"))
		require.NoError(t, err)
		assert.True(t, isInitFileExists)
		//
		isPropertiesFileExists, err := pathutil.NewPathChecker().IsPathExists(filepath.Join(tmpGradleHomeDir, "gradle.properties"))
		require.NoError(t, err)
		assert.True(t, isPropertiesFileExists)
	})
}
