// nolint: goconst, cyclop, maintidx
package xcelerate_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/internal/utils/mocks"
)

func TestConfig_Save(t *testing.T) {
	temp := t.TempDir()

	osProxy := func() *utilsMocks.OsProxyMock {
		return &utilsMocks.OsProxyMock{
			UserHomeDirFunc: func() (string, error) {
				return temp, nil
			},
			MkdirAllFunc: os.MkdirAll,
			CreateFunc:   os.Create,
			StatFunc:     os.Stat,
		}
	}

	encoder := func() *utilsMocks.EncoderMock {
		return &utilsMocks.EncoderMock{
			SetIndentFunc:     func(_ string, _ string) {},
			SetEscapeHTMLFunc: func(_ bool) {},
			EncodeFunc:        func(_ any) error { return nil },
		}
	}

	encoderFactory := func() *utilsMocks.EncoderFactoryMock {
		return &utilsMocks.EncoderFactoryMock{
			EncoderFunc: func(_ io.Writer) utils.Encoder {
				return encoder()
			},
		}
	}

	t.Run("When no error save will succeed", func(t *testing.T) {
		// given
		mockEncoder := encoder()
		mockEncoderFactory := &utilsMocks.EncoderFactoryMock{
			EncoderFunc: func(_ io.Writer) utils.Encoder {
				return mockEncoder
			},
		}
		mockOsProxy := osProxy()
		t.Cleanup(func() {
			_ = os.Remove(xcelerate.PathFor(mockOsProxy, "config.json"))
		})

		// when
		config := xcelerate.Config{
			ProxyVersion:           "1.0.0",
			WrapperVersion:         "1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild",
			BuildCacheEnabled:      true,
			DebugLogging:           true,
		}
		err := config.Save(mockLogger, mockOsProxy, mockEncoderFactory)

		// then
		require.NoError(t, err)

		require.Len(t, mockOsProxy.MkdirAllCalls(), 1)

		assert.Equal(t, xcelerate.DirPath(mockOsProxy), mockOsProxy.MkdirAllCalls()[0].Name)
		require.Len(t, mockOsProxy.CreateCalls(), 1)
		assert.Equal(t, xcelerate.PathFor(mockOsProxy, "config.json"), mockOsProxy.CreateCalls()[0].Name)
		require.Len(t, mockEncoder.SetIndentCalls(), 1)
		assert.Empty(t, mockEncoder.SetIndentCalls()[0].Prefix)
		assert.Equal(t, "  ", mockEncoder.SetIndentCalls()[0].Indent)
		require.Len(t, mockEncoder.SetEscapeHTMLCalls(), 1)
		assert.False(t, mockEncoder.SetEscapeHTMLCalls()[0].Escape)
		require.Len(t, mockEncoder.EncodeCalls(), 1)
		assert.Equal(t, config, mockEncoder.EncodeCalls()[0].Data)
	})

	t.Run("When error occurs making directories save returns an error", func(t *testing.T) {
		// given
		mockOsProxy := &utilsMocks.OsProxyMock{
			UserHomeDirFunc: func() (string, error) {
				return os.TempDir(), nil
			},
			MkdirAllFunc: func(_ string, _ os.FileMode) error {
				return os.ErrNotExist
			},
			StatFunc: os.Stat,
		}
		t.Cleanup(func() {
			_ = os.Remove(xcelerate.PathFor(mockOsProxy, "config.json"))
		})

		// when
		config := xcelerate.Config{
			ProxyVersion:           "1.0.0",
			WrapperVersion:         "1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild",
			BuildCacheEnabled:      true,
		}
		err := config.Save(mockLogger, mockOsProxy, encoderFactory())

		// then
		assert.EqualError(t, err, fmt.Errorf(xcelerate.ErrFmtCreateFolder, xcelerate.DirPath(mockOsProxy), os.ErrNotExist).Error())
	})

	t.Run("When error occurs when creating config file, it returns an error", func(t *testing.T) {
		// given
		mockOsProxy := &utilsMocks.OsProxyMock{
			UserHomeDirFunc: func() (string, error) {
				return os.TempDir(), nil
			},
			MkdirAllFunc: func(_ string, _ os.FileMode) error {
				return nil
			},
			CreateFunc: func(_ string) (*os.File, error) {
				return nil, os.ErrNotExist
			},
			StatFunc: os.Stat,
		}

		// when
		config := xcelerate.Config{
			ProxyVersion:           "1.0.0",
			WrapperVersion:         "1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild",
			BuildCacheEnabled:      true,
		}
		err := config.Save(mockLogger, mockOsProxy, encoderFactory())

		// then
		assert.EqualError(t, err, fmt.Errorf(xcelerate.ErrFmtCreateConfigFile, os.ErrNotExist).Error())
	})

	t.Run("When error occurs when encoding config file, it returns an error", func(t *testing.T) {
		// given
		encodingError := errors.New("encoding error")

		mockEncoder := &utilsMocks.EncoderMock{
			SetIndentFunc:     func(_ string, _ string) {},
			SetEscapeHTMLFunc: func(_ bool) {},
			EncodeFunc:        func(_ any) error { return encodingError },
		}

		mockEncoderFactory := &utilsMocks.EncoderFactoryMock{
			EncoderFunc: func(_ io.Writer) utils.Encoder {
				return mockEncoder
			},
		}

		// when
		config := xcelerate.Config{
			ProxyVersion:           "1.0.0",
			WrapperVersion:         "1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild",
			BuildCacheEnabled:      true,
		}
		err := config.Save(mockLogger, osProxy(), mockEncoderFactory)

		// then
		assert.EqualError(t, err, fmt.Errorf(xcelerate.ErrFmtEncodeConfigFile, encodingError).Error())
	})
}

func TestConfig_NewConfig(t *testing.T) {
	t.Run("When auth env vars are not set, returns error", func(t *testing.T) {
		osProxyMock := &utilsMocks.OsProxyMock{}

		_, err := xcelerate.NewConfig(context.Background(), nil, xcelerate.Params{}, map[string]string{}, osProxyMock, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), fmt.Errorf(xcelerate.ErrNoAuthConfig, errors.New("")).Error())
	})
	t.Run("When all paths are defined, returns new config", func(t *testing.T) {
		envs := map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":      "auth-token",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID":    "workspace-id",
			"BITRISE_BUILD_CACHE_CLI_VERSION":     "cli-version-1.0.0",
			"BITRISE_XCELERATE_PROXY_VERSION":     "proxy-version-1.0.0",
			"BITRISE_XCELERATE_WRAPPER_VERSION":   "wrapper-version-1.0.0",
			"BITRISE_XCELERATE_PROXY_SOCKET_PATH": "/tmp/xcelerate-proxy.sock",
		}

		cmdMock := &utilsMocks.CommandMock{
			CombinedOutputFunc: func() ([]byte, error) {
				return []byte("/usr/bin/xcodebuild2"), nil
			},
		}

		osProxyMock := &utilsMocks.OsProxyMock{
			TempDirFunc: func() string {
				return t.TempDir()
			},
		}

		actual, err := xcelerate.NewConfig(context.Background(), mockLogger, xcelerate.Params{
			BuildCacheEnabled: true,
			DebugLogging:      true,
		}, envs, osProxyMock, func(_ context.Context, command string, args ...string) utils.Command {
			assert.Equal(t, "which", command)
			require.Len(t, args, 1)
			assert.Equal(t, "xcodebuild", args[0])

			return cmdMock
		})
		require.NoError(t, err)

		expected := xcelerate.Config{
			ProxyVersion:           "proxy-version-1.0.0",
			ProxySocketPath:        "/tmp/xcelerate-proxy.sock",
			WrapperVersion:         "wrapper-version-1.0.0",
			CLIVersion:             "cli-version-1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild2",
			BuildCacheEndpoint:     "grpcs://bitrise-accelerate.services.bitrise.io",
			BuildCacheEnabled:      true,
			DebugLogging:           true,
			AuthConfig: common.CacheAuthConfig{
				AuthToken:   "auth-token",
				WorkspaceID: "workspace-id",
			},
		}
		require.Len(t, cmdMock.CombinedOutputCalls(), 1)
		assert.Equal(t, expected, actual)
	})

	t.Run("When silent and debug logging enabled, silent will take precedence", func(t *testing.T) {
		envs := map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":      "auth-token",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID":    "workspace-id",
			"BITRISE_BUILD_CACHE_CLI_VERSION":     "cli-version-1.0.0",
			"BITRISE_XCELERATE_PROXY_VERSION":     "proxy-version-1.0.0",
			"BITRISE_XCELERATE_WRAPPER_VERSION":   "wrapper-version-1.0.0",
			"BITRISE_XCELERATE_PROXY_SOCKET_PATH": "/tmp/xcelerate-proxy.sock",
		}

		cmdMock := &utilsMocks.CommandMock{
			CombinedOutputFunc: func() ([]byte, error) {
				return []byte("/usr/bin/xcodebuild2"), nil
			},
		}

		osProxyMock := &utilsMocks.OsProxyMock{
			TempDirFunc: func() string {
				return t.TempDir()
			},
		}

		actual, err := xcelerate.NewConfig(context.Background(), mockLogger, xcelerate.Params{
			BuildCacheEnabled: true,
			DebugLogging:      true,
			Silent:            true,
		}, envs, osProxyMock, func(_ context.Context, command string, args ...string) utils.Command {
			return cmdMock
		})
		require.NoError(t, err)

		expected := xcelerate.Config{
			ProxyVersion:           "proxy-version-1.0.0",
			ProxySocketPath:        "/tmp/xcelerate-proxy.sock",
			WrapperVersion:         "wrapper-version-1.0.0",
			CLIVersion:             "cli-version-1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild2",
			BuildCacheEndpoint:     "grpcs://bitrise-accelerate.services.bitrise.io",
			BuildCacheEnabled:      true,
			DebugLogging:           false,
			Silent:                 true,
			AuthConfig: common.CacheAuthConfig{
				AuthToken:   "auth-token",
				WorkspaceID: "workspace-id",
			},
		}
		require.Len(t, cmdMock.CombinedOutputCalls(), 1)
		assert.Equal(t, expected, actual)
	})

	t.Run("When xcode path is overridden, returns config with that path", func(t *testing.T) {
		envs := map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "auth-token",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "workspace-id",
		}

		osProxyMock := &utilsMocks.OsProxyMock{
			TempDirFunc: func() string {
				return t.TempDir()
			},
		}

		cmdMock := &utilsMocks.CommandMock{
			CombinedOutputFunc: func() ([]byte, error) {
				return []byte("something-else"), errors.New("something went wrong")
			},
		}

		actual, err := xcelerate.NewConfig(context.Background(), mockLogger, xcelerate.Params{
			BuildCacheEnabled:       true,
			DebugLogging:            true,
			BuildCacheEndpoint:      "grpcs://bitrise-accelerate.services.bitrise.io",
			XcodePathOverride:       "/usr/bin/xcodebuild-override",
			ProxySocketPathOverride: "/tmp/xcelerate-proxy.sock",
		}, envs, osProxyMock, func(_ context.Context, _ string, _ ...string) utils.Command {
			return cmdMock
		})
		require.NoError(t, err)

		expected := xcelerate.Config{
			ProxyVersion:           "",
			WrapperVersion:         "",
			CLIVersion:             "",
			BuildCacheEndpoint:     "grpcs://bitrise-accelerate.services.bitrise.io",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild-override",
			ProxySocketPath:        "/tmp/xcelerate-proxy.sock",
			BuildCacheEnabled:      true,
			DebugLogging:           true,
			AuthConfig: common.CacheAuthConfig{
				AuthToken:   "auth-token",
				WorkspaceID: "workspace-id",
			},
		}

		assert.Equal(t, expected, actual)
	})

	t.Run("When `which xcodebuild` command fails, returns config with default path", func(t *testing.T) {
		envs := map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "auth-token",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "workspace-id",
		}

		osProxyMock := &utilsMocks.OsProxyMock{
			TempDirFunc: func() string {
				return "my-temp-dir"
			},
		}

		cmdMock := &utilsMocks.CommandMock{
			CombinedOutputFunc: func() ([]byte, error) {
				return []byte("something-else"), errors.New("something went wrong")
			},
		}

		actual, err := xcelerate.NewConfig(context.Background(), mockLogger, xcelerate.Params{
			BuildCacheEnabled: true,
			DebugLogging:      true,
		}, envs, osProxyMock, func(_ context.Context, _ string, _ ...string) utils.Command {
			return cmdMock
		})
		require.NoError(t, err)

		expected := xcelerate.Config{
			ProxyVersion:           "",
			ProxySocketPath:        "my-temp-dir/xcelerate-proxy.sock",
			WrapperVersion:         "",
			CLIVersion:             "",
			BuildCacheEndpoint:     "grpcs://bitrise-accelerate.services.bitrise.io",
			OriginalXcodebuildPath: xcelerate.DefaultXcodePath,
			BuildCacheEnabled:      true,
			DebugLogging:           true,
			AuthConfig: common.CacheAuthConfig{
				AuthToken:   "auth-token",
				WorkspaceID: "workspace-id",
			},
		}

		assert.Equal(t, expected, actual)
	})

	t.Run("When build cache url is overridden, returns config with that url", func(t *testing.T) {
		envs := map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "auth-token",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "workspace-id",
		}

		osProxyMock := &utilsMocks.OsProxyMock{
			TempDirFunc: func() string {
				return "my-temp-dir"
			},
		}

		cmdMock := &utilsMocks.CommandMock{
			CombinedOutputFunc: func() ([]byte, error) {
				return []byte("something-else"), errors.New("something went wrong")
			},
		}

		actual, err := xcelerate.NewConfig(context.Background(), mockLogger, xcelerate.Params{
			BuildCacheEnabled:  true,
			DebugLogging:       true,
			BuildCacheEndpoint: "grpc://localhost:6666",
		}, envs, osProxyMock, func(_ context.Context, _ string, _ ...string) utils.Command {
			return cmdMock
		})
		require.NoError(t, err)

		expected := xcelerate.Config{
			ProxyVersion:           "",
			WrapperVersion:         "",
			CLIVersion:             "",
			BuildCacheEndpoint:     "grpc://localhost:6666",
			OriginalXcodebuildPath: xcelerate.DefaultXcodePath,
			ProxySocketPath:        "my-temp-dir/xcelerate-proxy.sock",
			BuildCacheEnabled:      true,
			DebugLogging:           true,
			AuthConfig: common.CacheAuthConfig{
				AuthToken:   "auth-token",
				WorkspaceID: "workspace-id",
			},
		}

		assert.Equal(t, expected, actual)
	})

	t.Run("When build cache url is overridden in the env, returns config with that url", func(t *testing.T) {
		envs := map[string]string{
			"BITRISE_BUILD_CACHE_AUTH_TOKEN":   "auth-token",
			"BITRISE_BUILD_CACHE_WORKSPACE_ID": "workspace-id",
			"BITRISE_BUILD_CACHE_ENDPOINT":     "grpc://localhost:6666",
		}

		osProxyMock := &utilsMocks.OsProxyMock{
			TempDirFunc: func() string {
				return "my-temp-dir"
			},
		}

		cmdMock := &utilsMocks.CommandMock{
			CombinedOutputFunc: func() ([]byte, error) {
				return []byte("something-else"), errors.New("something went wrong")
			},
		}

		actual, err := xcelerate.NewConfig(context.Background(), mockLogger, xcelerate.Params{
			BuildCacheEnabled:  true,
			DebugLogging:       true,
			BuildCacheEndpoint: "",
		}, envs, osProxyMock, func(_ context.Context, _ string, _ ...string) utils.Command {
			return cmdMock
		})
		require.NoError(t, err)

		expected := xcelerate.Config{
			ProxyVersion:           "",
			WrapperVersion:         "",
			CLIVersion:             "",
			BuildCacheEndpoint:     "grpc://localhost:6666",
			OriginalXcodebuildPath: xcelerate.DefaultXcodePath,
			ProxySocketPath:        "my-temp-dir/xcelerate-proxy.sock",
			BuildCacheEnabled:      true,
			DebugLogging:           true,
			AuthConfig: common.CacheAuthConfig{
				AuthToken:   "auth-token",
				WorkspaceID: "workspace-id",
			},
		}

		assert.Equal(t, expected, actual)
	})
}
