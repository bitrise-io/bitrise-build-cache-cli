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
	osProxy := func() *utilsMocks.OsProxyMock {
		return &utilsMocks.OsProxyMock{
			UserHomeDirFunc: func() (string, error) {
				return "~", nil
			},
			MkdirAllFunc: func(_ string, _ os.FileMode) error {
				return nil
			},
			CreateFunc: func(_ string) (*os.File, error) {
				return &os.File{}, nil
			},
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
		assert.Equal(t, xcelerate.DirPath(), mockOsProxy.MkdirAllCalls()[0].Pth)
		require.Len(t, mockOsProxy.CreateCalls(), 1)
		assert.Equal(t, xcelerate.PathFor("config.json"), mockOsProxy.CreateCalls()[0].Pth)
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
				return "~", nil
			},
			MkdirAllFunc: func(_ string, _ os.FileMode) error {
				return os.ErrNotExist
			},
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
		assert.EqualError(t, err, fmt.Errorf(xcelerate.ErrFmtCreateFolder, xcelerate.DirPath(), os.ErrNotExist).Error())
	})

	t.Run("When error occurs when creating config file, it returns an error", func(t *testing.T) {
		// given
		mockOsProxy := &utilsMocks.OsProxyMock{
			UserHomeDirFunc: func() (string, error) {
				return "~", nil
			},
			MkdirAllFunc: func(_ string, _ os.FileMode) error {
				return nil
			},
			CreateFunc: func(_ string) (*os.File, error) {
				return nil, os.ErrNotExist
			},
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
	t.Run("When `where xcodebuild` command returns path, returns new config", func(t *testing.T) {
		envMock := func(s string) string {
			switch s {
			case "BITRISE_BUILD_CACHE_CLI_VERSION":
				return "cli-version-1.0.0"
			case "BITRISE_XCELERATE_PROXY_VERSION":
				return "proxy-version-1.0.0"
			case "BITRISE_XCELERATE_WRAPPER_VERSION":
				return "wrapper-version-1.0.0"
			}

			return ""
		}

		cmdMock := &utilsMocks.CommandMock{
			CombinedOutputFunc: func() ([]byte, error) {
				return []byte("/usr/bin/xcodebuild2"), nil
			},
		}

		actual := xcelerate.NewConfig(context.Background(), mockLogger, xcelerate.Params{
			BuildCacheEnabled: true,
			DebugLogging:      true,
		}, envMock, func(_ context.Context, command string, args ...string) utils.Command {
			assert.Equal(t, "which", command)
			require.Len(t, args, 1)
			assert.Equal(t, "xcodebuild", args[0])

			return cmdMock
		})

		expected := xcelerate.Config{
			ProxyVersion:           "proxy-version-1.0.0",
			WrapperVersion:         "wrapper-version-1.0.0",
			CLIVersion:             "cli-version-1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild2",
			BuildCacheEnabled:      true,
			DebugLogging:           true,
		}
		require.Len(t, cmdMock.CombinedOutputCalls(), 1)
		assert.Equal(t, expected, actual)
	})

	t.Run("When `which xcodebuild` command fails, returns config with default path", func(t *testing.T) {
		envMock := func(s string) string {
			return ""
		}

		cmdMock := &utilsMocks.CommandMock{
			CombinedOutputFunc: func() ([]byte, error) {
				return []byte("something-else"), errors.New("something went wrong")
			},
		}

		actual := xcelerate.NewConfig(context.Background(), mockLogger, xcelerate.Params{
			BuildCacheEnabled: true,
			DebugLogging:      true,
		}, envMock, func(_ context.Context, _ string, _ ...string) utils.Command {
			return cmdMock
		})

		expected := xcelerate.Config{
			ProxyVersion:           "",
			WrapperVersion:         "",
			CLIVersion:             "",
			OriginalXcodebuildPath: xcelerate.DefaultXcodePath,
			BuildCacheEnabled:      true,
			DebugLogging:           true,
		}

		assert.Equal(t, expected, actual)
	})
}
