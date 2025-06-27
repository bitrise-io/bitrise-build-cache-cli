package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/bazel"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
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
		mockLogger.On("Errorf", mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()
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
		require.EqualError(t, err, "template inventory error: read auth config from environment variables: BITRISE_BUILD_CACHE_AUTH_TOKEN or BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN environment variable not set")
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
		bazelrcPath := filepath.Join(tmpHomeDir, ".bazelrc")
		isBazelrcExists, err := pathutil.NewPathChecker().IsPathExists(bazelrcPath)
		require.NoError(t, err)
		assert.True(t, isBazelrcExists)
	})

	t.Run("~/.bazelrc file already exists", func(t *testing.T) {
		mockLogger, tmpHomeDir := prep()
		bazelrcPath := filepath.Join(tmpHomeDir, ".bazelrc")
		originalBazelrcContent := `# original bazelrc content
# multi line`
		err := os.WriteFile(bazelrcPath, []byte(originalBazelrcContent), 0755) //nolint:gosec
		require.NoError(t, err)

		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})
		err = enableForBazelCmdFn(mockLogger, tmpHomeDir, envVars)

		// then
		require.NoError(t, err)
		// the original content of bazelrc should still be in the file
		bazelrcContent, err := os.ReadFile(bazelrcPath)
		require.NoError(t, err)
		assert.Contains(t, string(bazelrcContent), originalBazelrcContent)
		assert.True(t, strings.HasPrefix(string(bazelrcContent), originalBazelrcContent))
		// followed by the generated content block
		assert.Contains(t, string(bazelrcContent), "# [start] generated-by-bitrise-build-cache")
		assert.Contains(t, string(bazelrcContent), "# [end] generated-by-bitrise-build-cache")
		assert.True(t, strings.HasSuffix(string(bazelrcContent), "# [end] generated-by-bitrise-build-cache\n"))
	})

	t.Run("with timestamps enabled", func(t *testing.T) {
		mockLogger, tmpHomeDir := prep()
		timestamps = true
		defer func() { timestamps = false }()

		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})
		err := enableForBazelCmdFn(mockLogger, tmpHomeDir, envVars)

		// then
		require.NoError(t, err)
		bazelrcPath := filepath.Join(tmpHomeDir, ".bazelrc")
		bazelrcContent, err := os.ReadFile(bazelrcPath)
		require.NoError(t, err)
		assert.Contains(t, string(bazelrcContent), "build --show_timestamps")
	})

	t.Run("with timestamps disabled", func(t *testing.T) {
		mockLogger, tmpHomeDir := prep()
		timestamps = false

		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})
		err := enableForBazelCmdFn(mockLogger, tmpHomeDir, envVars)

		// then
		require.NoError(t, err)
		bazelrcPath := filepath.Join(tmpHomeDir, ".bazelrc")
		bazelrcContent, err := os.ReadFile(bazelrcPath)
		require.NoError(t, err)
		assert.NotContains(t, string(bazelrcContent), "build --show_timestamps")
	})

	t.Run("existing bitrise block gets updated", func(t *testing.T) {
		mockLogger, tmpHomeDir := prep()
		bazelrcPath := filepath.Join(tmpHomeDir, ".bazelrc")

		existingContent := `# existing content
# [start] generated-by-bitrise-build-cache
build --remote_cache=oldurl
build --remote_upload_local_results
# [end] generated-by-bitrise-build-cache
# other content`
		err := os.WriteFile(bazelrcPath, []byte(existingContent), 0755) //nolint:gosec
		require.NoError(t, err)

		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})
		err = enableForBazelCmdFn(mockLogger, tmpHomeDir, envVars)

		// then
		require.NoError(t, err)
		bazelrcContent, err := os.ReadFile(bazelrcPath)
		require.NoError(t, err)
		// Check that the original content is preserved
		assert.Contains(t, string(bazelrcContent), "# existing content")
		assert.Contains(t, string(bazelrcContent), "# other content")
		// Check that the old block is updated
		assert.NotContains(t, string(bazelrcContent), "build --remote_cache=oldurl")
		// Check that new content is present
		assert.Contains(t, string(bazelrcContent), "build --remote_cache=")
		assert.Contains(t, string(bazelrcContent), "build --remote_upload_local_results")
		// Verify block markers
		assert.Contains(t, string(bazelrcContent), "# [start] generated-by-bitrise-build-cache")
		assert.Contains(t, string(bazelrcContent), "# [end] generated-by-bitrise-build-cache")
	})

	t.Run("with cache push disabled", func(t *testing.T) {
		mockLogger, tmpHomeDir := prep()
		bazelrcPath := filepath.Join(tmpHomeDir, ".bazelrc")

		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})

		// Create params and disable push
		params := bazelconfig.DefaultActivateBazelParams()
		params.Cache.Enabled = true
		params.Cache.PushEnabled = false

		// First invoke with push disabled
		inventory, err := params.TemplateInventory(mockLogger, envVars, func(_ string, _ ...string) (string, error) {
			return "", nil
		}, false)
		require.NoError(t, err)

		bazelrcBlockContent, err := inventory.GenerateBazelrc(utils.DefaultTemplateProxy())
		require.NoError(t, err)

		err = os.WriteFile(bazelrcPath, []byte(bazelrcBlockContent), 0755) //nolint:gosec
		require.NoError(t, err)

		// Check the content
		bazelrcContent, err := os.ReadFile(bazelrcPath)
		require.NoError(t, err)

		// Should have remote cache enabled but push disabled
		assert.Contains(t, string(bazelrcContent), "build --remote_cache=")
		assert.Contains(t, string(bazelrcContent), "build --noremote_upload_local_results")
		assert.NotContains(t, string(bazelrcContent), "build --remote_upload_local_results")
	})

	t.Run("existing bitrise block with timestamps gets updated without timestamps", func(t *testing.T) {
		mockLogger, tmpHomeDir := prep()
		bazelrcPath := filepath.Join(tmpHomeDir, ".bazelrc")

		existingContent := `# existing content
# [start] generated-by-bitrise-build-cache
build --remote_cache=oldurl
build --remote_upload_local_results
build --show_timestamps
# [end] generated-by-bitrise-build-cache
# other content`
		err := os.WriteFile(bazelrcPath, []byte(existingContent), 0755) //nolint:gosec
		require.NoError(t, err)

		// Make sure timestamps is disabled
		timestamps = false

		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})
		err = enableForBazelCmdFn(mockLogger, tmpHomeDir, envVars)

		// then
		require.NoError(t, err)
		bazelrcContent, err := os.ReadFile(bazelrcPath)
		require.NoError(t, err)

		// Check that timestamps flag is removed
		assert.NotContains(t, string(bazelrcContent), "build --show_timestamps")
		// Check that new content is present
		assert.Contains(t, string(bazelrcContent), "build --remote_cache=")
		assert.Contains(t, string(bazelrcContent), "build --remote_upload_local_results")
		// Verify block markers
		assert.Contains(t, string(bazelrcContent), "# [start] generated-by-bitrise-build-cache")
		assert.Contains(t, string(bazelrcContent), "# [end] generated-by-bitrise-build-cache")
	})
}
