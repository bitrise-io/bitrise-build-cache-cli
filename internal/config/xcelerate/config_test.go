package xcelerate_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"context"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/internal/utils/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		err := config.Save(mockOsProxy, mockEncoderFactory)

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

		// second call to save should return an error
		assert.ErrorIs(t, config.Save(mockOsProxy, mockEncoderFactory), xcelerate.ErrConfigFileAlreadyExists)
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
		err := config.Save(mockOsProxy, encoderFactory())

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
		err := config.Save(mockOsProxy, encoderFactory())

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
		err := config.Save(osProxy(), mockEncoderFactory)

		// then
		assert.EqualError(t, err, fmt.Errorf(xcelerate.ErrFmtEncodeConfigFile, encodingError).Error())
	})
}

func TestConfig_NewConfig(t *testing.T) {
	t.Run("When all paths are defined, returns new config", func(t *testing.T) {
		envMock := func(s string) string {
			switch s {
			case "BITRISE_BUILD_CACHE_CLI_VERSION":
				return "cli-version-1.0.0"
			case "BITRISE_XCELERATE_PROXY_VERSION":
				return "proxy-version-1.0.0"
			case "BITRISE_XCELERATE_WRAPPER_VERSION":
				return "wrapper-version-1.0.0"
			case "BITRISE_XCELERATE_PROXY_SOCKET_PATH":
				return "/tmp/xcelerate-proxy.sock"
			}

			return ""
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

		actual := xcelerate.NewConfig(context.Background(), mockLogger, xcelerate.Params{
			BuildCacheEnabled: true,
			DebugLogging:      true,
		}, envMock, osProxyMock, func(_ context.Context, command string, args ...string) utils.Command {
			assert.Equal(t, "which", command)
			require.Len(t, args, 1)
			assert.Equal(t, "xcodebuild", args[0])

			return cmdMock
		})

		expected := xcelerate.Config{
			ProxyVersion:           "proxy-version-1.0.0",
			ProxySocketPath:        "/tmp/xcelerate-proxy.sock",
			WrapperVersion:         "wrapper-version-1.0.0",
			CLIVersion:             "cli-version-1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild2",
			BuildCacheEnabled:      true,
			DebugLogging:           true,
		}
		require.Len(t, cmdMock.CombinedOutputCalls(), 1)
		assert.Equal(t, expected, actual)
	})

	t.Run("When xcode path is overridden, returns config with that path", func(t *testing.T) {
		envMock := func(s string) string {
			return ""
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

		actual := xcelerate.NewConfig(context.Background(), mockLogger, xcelerate.Params{
			BuildCacheEnabled:       true,
			DebugLogging:            true,
			XcodePathOverride:       "/usr/bin/xcodebuild-override",
			ProxySocketPathOverride: "/tmp/xcelerate-proxy.sock",
		}, envMock, osProxyMock, func(_ context.Context, _ string, _ ...string) utils.Command {
			return cmdMock
		})

		expected := xcelerate.Config{
			ProxyVersion:           "",
			WrapperVersion:         "",
			CLIVersion:             "",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild-override",
			ProxySocketPath:        "/tmp/xcelerate-proxy.sock",
			BuildCacheEnabled:      true,
			DebugLogging:           true,
		}

		assert.Equal(t, expected, actual)
	})

	t.Run("When `which xcodebuild` command fails, returns config with default path", func(t *testing.T) {
		envMock := func(s string) string {
			return ""
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

		actual := xcelerate.NewConfig(context.Background(), mockLogger, xcelerate.Params{
			BuildCacheEnabled:       true,
			DebugLogging:            true,
			ProxySocketPathOverride: "/tmp/xcelerate-proxy.sock",
		}, envMock, osProxyMock, func(_ context.Context, _ string, _ ...string) utils.Command {
			return cmdMock
		})

		expected := xcelerate.Config{
			ProxyVersion:           "",
			ProxySocketPath:        "/tmp/xcelerate-proxy.sock",
			WrapperVersion:         "",
			CLIVersion:             "",
			OriginalXcodebuildPath: xcelerate.DefaultXcodePath,
			BuildCacheEnabled:      true,
			DebugLogging:           true,
		}

		assert.Equal(t, expected, actual)
	})
}
