//nolint:dupl
package xcode_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/xcode"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils/mocks"
)

func TestActivateXcode_activateXcodeCmdFn(t *testing.T) {
	home := t.TempDir()

	envs := map[string]string{
		"HOME":                             home,
		"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "token",
		"BITRISE_BUILD_CACHE_WORKSPACE_ID": "abc123",
		"GITHUB_ENV":                       filepath.Join(home, ".github_env"),
	}

	t.Run("success", func(t *testing.T) {
		osProxy := &utilsMocks.OsProxyMock{
			ReadFileIfExistsFunc: utils.DefaultOsProxy{}.ReadFileIfExists,
			UserHomeDirFunc: func() (string, error) {
				return home, nil
			},
			MkdirAllFunc:  os.MkdirAll,
			CreateFunc:    os.Create,
			OpenFileFunc:  os.OpenFile,
			WriteFileFunc: os.WriteFile,
		}

		err := xcode.ActivateXcodeCommandFn(
			context.Background(), mockLogger, osProxy, func(_ context.Context, _ string, _ ...string) utils.Command {
				return &utilsMocks.CommandMock{}
			},
			utils.DefaultEncoderFactory{},
			utils.DefaultDecoderFactory{},
			xcelerate.Params{
				BuildCacheEnabled:       true,
				DebugLogging:            true,
				XcodePathOverride:       "/xxx/xcodebuild",
				XcrunPathOverride:       "/xxx/xcrun",
				ProxySocketPathOverride: "/xxx/xcelerate.sock",
			},
			envs,
		)

		mockLogger.AssertCalled(t, "TInfof", xcode.ActivateXcodeSuccessful)
		require.NoError(t, err)

		// make sure files were created
		assert.FileExists(t, filepath.Join(home, ".bashrc"))
		assert.FileExists(t, filepath.Join(home, ".zshrc"))
		assert.FileExists(t, filepath.Join(home, ".github_env"))
		assert.FileExists(t, xcelerate.PathFor(osProxy, filepath.Join(xcelerate.BinDir, "bitrise-build-cache-cli")))
		assert.FileExists(t, xcelerate.PathFor(osProxy, filepath.Join(xcelerate.BinDir, "xcodebuild")))

		// make sure config was saved as expected
		config, err := xcelerate.ReadConfig(osProxy, utils.DefaultDecoderFactory{})
		require.NoError(t, err)
		require.NotNil(t, config)
		require.True(t, config.BuildCacheEnabled)
		require.True(t, config.DebugLogging)
		require.Equal(t, "/xxx/xcodebuild", config.OriginalXcodebuildPath)
		require.Equal(t, "/xxx/xcrun", config.OriginalXcrunPath)
		require.Equal(t, "/xxx/xcelerate.sock", config.ProxySocketPath)
		require.Equal(t, "token", config.AuthConfig.AuthToken)
		require.Equal(t, "abc123", config.AuthConfig.WorkspaceID)

		// let's call activate again to make sure already configured xcodebuild path is respected from existing config
		err = xcode.ActivateXcodeCommandFn(
			context.Background(),
			mockLogger,
			osProxy,
			func(ctx context.Context, command string, args ...string) utils.Command {
				return &utilsMocks.CommandMock{}
			},
			utils.DefaultEncoderFactory{},
			utils.DefaultDecoderFactory{},
			xcelerate.Params{
				BuildCacheEnabled:       true,
				DebugLogging:            true,
				ProxySocketPathOverride: "/xxx/xcelerate.sock",
			},
			envs,
		)
		require.NoError(t, err)

		// make sure config was saved as expected
		config, err = xcelerate.ReadConfig(osProxy, utils.DefaultDecoderFactory{})
		require.NoError(t, err)
		require.NotNil(t, config)
		require.True(t, config.BuildCacheEnabled)
		require.True(t, config.DebugLogging)
		require.Equal(t, "/xxx/xcodebuild", config.OriginalXcodebuildPath)
		require.Equal(t, "/xxx/xcrun", config.OriginalXcrunPath)
		require.Equal(t, "/xxx/xcelerate.sock", config.ProxySocketPath)
		require.Equal(t, "token", config.AuthConfig.AuthToken)
		require.Equal(t, "abc123", config.AuthConfig.WorkspaceID)
	})

	t.Run("When config save returns error activateXcodeCmdFn fails", func(t *testing.T) {
		expectedError := errors.New("failed to save config")

		osProxy := &utilsMocks.OsProxyMock{
			ReadFileIfExistsFunc: utils.DefaultOsProxy{}.ReadFileIfExists,
			UserHomeDirFunc: func() (string, error) {
				return home, nil
			},
			MkdirAllFunc: os.MkdirAll,
			CreateFunc: func(name string) (*os.File, error) {
				return nil, expectedError
			},
			OpenFileFunc: os.OpenFile,
			TempDirFunc:  os.TempDir,
		}

		err := xcode.ActivateXcodeCommandFn(
			context.Background(),
			mockLogger,
			osProxy,
			func(ctx context.Context, command string, args ...string) utils.Command {
				return &utilsMocks.CommandMock{}
			},
			utils.DefaultEncoderFactory{},
			utils.DefaultDecoderFactory{},
			xcelerate.Params{},
			envs,
		)

		assert.ErrorIs(t, err, expectedError)
	})
}
