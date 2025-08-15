package xcelerate_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_activateXcodeCmdFn(t *testing.T) {
	osProxy := func() *mocks.OsProxyMock {
		return &mocks.OsProxyMock{
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

	encoder := func() *mocks.EncoderMock {
		return &mocks.EncoderMock{
			SetIndentFunc:     func(_ string, _ string) {},
			SetEscapeHTMLFunc: func(_ bool) {},
			EncodeFunc:        func(_ any) error { return nil },
		}
	}

	encoderFactory := func() *mocks.EncoderFactoryMock {
		return &mocks.EncoderFactoryMock{
			EncoderFunc: func(_ io.Writer) utils.Encoder {
				return encoder()
			},
		}
	}

	t.Run("When no error save will succeed", func(t *testing.T) {
		// given
		mockEncoder := encoder()
		mockEncoderFactory := &mocks.EncoderFactoryMock{
			EncoderFunc: func(_ io.Writer) utils.Encoder {
				return mockEncoder
			},
		}
		mockOsProxy := osProxy()

		// when
		config := xcelerate.DefaultConfig{
			ProxyVersion:           "1.0.0",
			WrapperVersion:         "1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild",
			BuildCacheEnabled:      true,
		}
		err := config.Save(mockOsProxy, mockEncoderFactory)

		// then
		require.NoError(t, err)

		require.Len(t, mockOsProxy.MkdirAllCalls(), 1)
		assert.Equal(t, xcelerate.XceleratePath(), mockOsProxy.MkdirAllCalls()[0].Pth)
		require.Len(t, mockOsProxy.CreateCalls(), 1)
		assert.Equal(t, xcelerate.XceleratePathFor("config.json"), mockOsProxy.CreateCalls()[0].Pth)
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
		mockOsProxy := &mocks.OsProxyMock{
			UserHomeDirFunc: func() (string, error) {
				return "~", nil
			},
			MkdirAllFunc: func(_ string, _ os.FileMode) error {
				return os.ErrNotExist
			},
		}

		// when
		config := xcelerate.DefaultConfig{
			ProxyVersion:           "1.0.0",
			WrapperVersion:         "1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild",
			BuildCacheEnabled:      true,
		}
		err := config.Save(mockOsProxy, encoderFactory())

		// then
		assert.EqualError(t, err, fmt.Errorf(xcelerate.ErrFmtCreateFolder, xcelerate.XceleratePath(), os.ErrNotExist).Error())
	})

	t.Run("When error occurs when creating config file, it returns an error", func(t *testing.T) {
		// given
		mockOsProxy := &mocks.OsProxyMock{
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
		config := xcelerate.DefaultConfig{
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

		mockEncoder := &mocks.EncoderMock{
			SetIndentFunc:     func(_ string, _ string) {},
			SetEscapeHTMLFunc: func(_ bool) {},
			EncodeFunc:        func(_ any) error { return encodingError },
		}

		mockEncoderFactory := &mocks.EncoderFactoryMock{
			EncoderFunc: func(_ io.Writer) utils.Encoder {
				return mockEncoder
			},
		}

		// when
		config := xcelerate.DefaultConfig{
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
