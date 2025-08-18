// nolint: cyclop, funlen, goconst, maintidx
package cmd_test

import (
	"os"
	"strings"
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd"
	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/bazel"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/internal/utils/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_enableForBazelCmdFn(t *testing.T) {
	// when
	t.Run("No envs specified", func(t *testing.T) {
		mockOsProxy := &utilsMocks.OsProxyMock{
			UserHomeDirFunc: func() (string, error) {
				return "/mock/home", nil
			},
		}
		envVars := createEnvProvider(map[string]string{})
		err := cmd.EnableForBazelCmdFn(mockLogger, mockOsProxy, envVars)

		// then
		require.EqualError(t, err, "template inventory error: read auth config from environment variables: BITRISE_BUILD_CACHE_AUTH_TOKEN or BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN environment variable not set")
	})

	t.Run("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN specified", func(t *testing.T) {
		mockOsProxy := &utilsMocks.OsProxyMock{
			UserHomeDirFunc: func() (string, error) {
				return "/mock/home", nil
			},
			ReadFileIfExistsFunc: func(pth string) (string, bool, error) {
				return "", false, nil // simulate file doesn't exist
			},
			WriteFileFunc: func(pth string, data []byte, mode os.FileMode) error {
				return nil // success
			},
		}
		envVars := createEnvProvider(map[string]string{
			"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": "ServiceAccessTokenValue",
		})
		err := cmd.EnableForBazelCmdFn(mockLogger, mockOsProxy, envVars)

		// then
		require.NoError(t, err)
		require.Len(t, mockOsProxy.UserHomeDirCalls(), 1)
		require.Len(t, mockOsProxy.ReadFileIfExistsCalls(), 1)
		require.Len(t, mockOsProxy.WriteFileCalls(), 1)
	})

	t.Run("BITRISE_BUILD_CACHE_WORKSPACE_ID and BITRISE_BUILD_CACHE_AUTH_TOKEN specified", func(t *testing.T) {
		mockOsProxy := &utilsMocks.OsProxyMock{
			UserHomeDirFunc: func() (string, error) {
				return "/mock/home", nil
			},
			ReadFileIfExistsFunc: func(pth string) (string, bool, error) {
				return "", false, nil // simulate file doesn't exist
			},
			WriteFileFunc: func(pth string, data []byte, mode os.FileMode) error {
				return nil // success
			},
		}
		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})
		err := cmd.EnableForBazelCmdFn(mockLogger, mockOsProxy, envVars)

		// then
		require.NoError(t, err)
		require.Len(t, mockOsProxy.UserHomeDirCalls(), 1)
		require.Len(t, mockOsProxy.ReadFileIfExistsCalls(), 1)
		require.Len(t, mockOsProxy.WriteFileCalls(), 1)
	})

	t.Run("~/.bazelrc file does not exist", func(t *testing.T) {
		mockOsProxy := &utilsMocks.OsProxyMock{
			UserHomeDirFunc: func() (string, error) {
				return "/mock/home", nil
			},
			ReadFileIfExistsFunc: func(pth string) (string, bool, error) {
				return "", false, nil // simulate file doesn't exist
			},
			WriteFileFunc: func(pth string, data []byte, mode os.FileMode) error {
				return nil // success
			},
		}
		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})
		err := cmd.EnableForBazelCmdFn(mockLogger, mockOsProxy, envVars)

		// then
		require.NoError(t, err)
		require.Len(t, mockOsProxy.UserHomeDirCalls(), 1)
		require.Len(t, mockOsProxy.ReadFileIfExistsCalls(), 1)
		require.Len(t, mockOsProxy.WriteFileCalls(), 1)
		// Verify the correct path is checked and written to
		assert.Equal(t, "/mock/home/.bazelrc", mockOsProxy.ReadFileIfExistsCalls()[0].Pth)
		assert.Equal(t, "/mock/home/.bazelrc", mockOsProxy.WriteFileCalls()[0].Pth)
		assert.NotEmpty(t, mockOsProxy.WriteFileCalls()[0].Data)
	})

	t.Run("~/.bazelrc file already exists", func(t *testing.T) {
		originalBazelrcContent := `# original bazelrc content
# multi line`

		mockOsProxy := &utilsMocks.OsProxyMock{
			UserHomeDirFunc: func() (string, error) {
				return "/mock/home", nil
			},
			ReadFileIfExistsFunc: func(pth string) (string, bool, error) {
				return originalBazelrcContent, true, nil // simulate file exists with original content
			},
			WriteFileFunc: func(pth string, data []byte, mode os.FileMode) error {
				return nil // success
			},
		}

		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})
		err := cmd.EnableForBazelCmdFn(mockLogger, mockOsProxy, envVars)

		// then
		require.NoError(t, err)
		require.Len(t, mockOsProxy.UserHomeDirCalls(), 1)
		require.Len(t, mockOsProxy.ReadFileIfExistsCalls(), 1)
		require.Len(t, mockOsProxy.WriteFileCalls(), 1)

		// Verify the correct path is checked and written to
		assert.Equal(t, "/mock/home/.bazelrc", mockOsProxy.ReadFileIfExistsCalls()[0].Pth)
		assert.Equal(t, "/mock/home/.bazelrc", mockOsProxy.WriteFileCalls()[0].Pth)

		// Verify content
		writtenContent := mockOsProxy.WriteFileCalls()[0].Data
		assert.NotEmpty(t, writtenContent)
		assert.Contains(t, string(writtenContent), originalBazelrcContent)
		assert.True(t, strings.HasPrefix(string(writtenContent), originalBazelrcContent))
		// followed by the generated content block
		assert.Contains(t, string(writtenContent), "# [start] generated-by-bitrise-build-cache")
		assert.Contains(t, string(writtenContent), "# [end] generated-by-bitrise-build-cache")
		assert.True(t, strings.HasSuffix(string(writtenContent), "# [end] generated-by-bitrise-build-cache\n"))
	})

	t.Run("existing bitrise block gets updated", func(t *testing.T) {
		existingContent := `# existing content
# [start] generated-by-bitrise-build-cache
build --remote_cache=oldurl
build --remote_upload_local_results
# [end] generated-by-bitrise-build-cache
# other content`

		mockOsProxy := &utilsMocks.OsProxyMock{
			UserHomeDirFunc: func() (string, error) {
				return "/mock/home", nil
			},
			ReadFileIfExistsFunc: func(pth string) (string, bool, error) {
				return existingContent, true, nil // simulate file exists with existing block
			},
			WriteFileFunc: func(pth string, data []byte, mode os.FileMode) error {
				return nil // success
			},
		}

		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})
		err := cmd.EnableForBazelCmdFn(mockLogger, mockOsProxy, envVars)

		// then
		require.NoError(t, err)
		require.Len(t, mockOsProxy.UserHomeDirCalls(), 1)
		require.Len(t, mockOsProxy.ReadFileIfExistsCalls(), 1)
		require.Len(t, mockOsProxy.WriteFileCalls(), 1)

		// Verify content
		writtenContent := mockOsProxy.WriteFileCalls()[0].Data
		assert.NotEmpty(t, writtenContent)
		// Check that the original content is preserved
		assert.Contains(t, string(writtenContent), "# existing content")
		assert.Contains(t, string(writtenContent), "# other content")
		// Check that the old block is updated
		assert.NotContains(t, string(writtenContent), "build --remote_cache=oldurl")
		// Check that new content is present
		assert.Contains(t, string(writtenContent), "build --remote_cache=")
		assert.Contains(t, string(writtenContent), "build --remote_upload_local_results")
		// Verify block markers
		assert.Contains(t, string(writtenContent), "# [start] generated-by-bitrise-build-cache")
		assert.Contains(t, string(writtenContent), "# [end] generated-by-bitrise-build-cache")
	})

	t.Run("with cache push disabled", func(t *testing.T) {
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

		// Verify that the generated content has push disabled
		assert.Contains(t, bazelrcBlockContent, "build --remote_cache=")
		assert.Contains(t, bazelrcBlockContent, "build --noremote_upload_local_results")
		assert.NotContains(t, bazelrcBlockContent, "build --remote_upload_local_results")
	})

	t.Run("existing bitrise block with timestamps gets updated without timestamps", func(t *testing.T) {
		proxyMock := &utilsMocks.OsProxyMock{
			UserHomeDirFunc: func() (string, error) {
				return "/mock/home", nil
			},
			ReadFileIfExistsFunc: func(pth string) (string, bool, error) {
				if strings.Contains(pth, ".bazelrc") {
					return `# existing content
# [start] generated-by-bitrise-build-cache
build --remote_cache=oldurl
build --remote_upload_local_results
build --show_timestamps
# [end] generated-by-bitrise-build-cache
# other content`, true, nil // simulate file exists
				}

				return "", false, os.ErrNotExist
			},
			WriteFileFunc: func(pth string, data []byte, mode os.FileMode) error {
				return nil // success
			},
		}

		envVars := createEnvProvider(map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "AuthTokenValue",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "WorkspaceIDValue",
		})
		err := cmd.EnableForBazelCmdFn(mockLogger, proxyMock, envVars)

		// then
		require.NoError(t, err)

		require.Len(t, proxyMock.UserHomeDirCalls(), 1)
		require.Len(t, proxyMock.ReadFileIfExistsCalls(), 1)
		require.Len(t, proxyMock.WriteFileCalls(), 1)

		writtenContent := proxyMock.WriteFileCalls()[0].Data
		// Check that timestamps flag is removed
		assert.NotContains(t, string(writtenContent), "build --show_timestamps")
		// Check that new content is present
		assert.Contains(t, string(writtenContent), "build --remote_cache=")
		assert.Contains(t, string(writtenContent), "build --remote_upload_local_results")
		// Verify block markers
		assert.Contains(t, string(writtenContent), "# [start] generated-by-bitrise-build-cache")
		assert.Contains(t, string(writtenContent), "# [end] generated-by-bitrise-build-cache")
	})
}
