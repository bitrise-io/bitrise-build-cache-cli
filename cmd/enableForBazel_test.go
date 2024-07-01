package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_enableForBazelCmdFn(t *testing.T) {
	// given
	prep := func() (log.Logger, string) {
		mockLogger := &mocks.Logger{}
		mockLogger.On("Infof", mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
		tmpHomeDir := t.TempDir()

		return mockLogger, tmpHomeDir
	}

	// when
	t.Run("No envs specified", func(t *testing.T) {
		mockLogger, tmpHomeDir := prep()
		envVars := createEnvProvider(map[string]string{})
		err := enableForBazelCmdFn(mockLogger, tmpHomeDir, envVars)

		// then
		require.EqualError(t, err, "read auth config from environments: BITRISE_BUILD_CACHE_AUTH_TOKEN or BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN environment variable not set")
	})

	t.Run("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN specified", func(t *testing.T) {
		mockLogger, tmpHomeDir := prep()
		envVars := createEnvProvider(map[string]string{
			"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": "ServiceAccessTokenValue",
		})
		err := enableForBazelCmdFn(mockLogger, tmpHomeDir, envVars)

		// then
		require.NoError(t, err)
	})

	t.Run("BITRISE_BUILD_CACHE_WORKSPACE_ID and BITRISE_BUILD_CACHE_AUTH_TOKEN specified", func(t *testing.T) {
		mockLogger, tmpHomeDir := prep()
		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})
		err := enableForBazelCmdFn(mockLogger, tmpHomeDir, envVars)

		// then
		require.NoError(t, err)
	})

	t.Run("~/.bazelrc file does not exist", func(t *testing.T) {
		mockLogger, tmpHomeDir := prep()
		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})
		err := enableForBazelCmdFn(mockLogger, tmpHomeDir, envVars)

		// then
		require.NoError(t, err)
		//
		bazelRCPath := filepath.Join(tmpHomeDir, ".bazelrc")
		isBazelrcExists, err := pathutil.NewPathChecker().IsPathExists(bazelRCPath)
		require.NoError(t, err)
		assert.True(t, isBazelrcExists)
	})

	t.Run("~/.bazelrc file already exists", func(t *testing.T) {
		mockLogger, tmpHomeDir := prep()
		bazelRCPath := filepath.Join(tmpHomeDir, ".bazelrc")
		originalBazelrcContent := `# original bazelrc content
# multi line`
		err := os.WriteFile(bazelRCPath, []byte(originalBazelrcContent), 0755) //nolint:gosec
		require.NoError(t, err)

		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})
		err = enableForBazelCmdFn(mockLogger, tmpHomeDir, envVars)

		// then
		require.NoError(t, err)
		// the original content of bazelrc should still be in the file
		bazelrcContent, err := os.ReadFile(bazelRCPath)
		require.NoError(t, err)
		assert.Contains(t, string(bazelrcContent), originalBazelrcContent)
		assert.True(t, strings.HasPrefix(string(bazelrcContent), originalBazelrcContent))
		// followed by the generated content block
		assert.Contains(t, string(bazelrcContent), "# [start] generated-by-bitrise-build-cache")
		assert.Contains(t, string(bazelrcContent), "# [end] generated-by-bitrise-build-cache")
		assert.True(t, strings.HasSuffix(string(bazelrcContent), "# [end] generated-by-bitrise-build-cache\n"))
	})
}
