package xcelerate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_activateXcodeCmdFn(t *testing.T) {
	osProxy := func() *utils.MockOsProxy {
		return &utils.MockOsProxy{
			UserHomeDirFunc: func() (string, error) {
				return "~", nil
			},
			MkdirAllFunc: func(path string, perm os.FileMode) error {
				return nil
			},
			CreateFunc: func(path string) (*os.File, error) {
				return &os.File{}, nil
			},
		}
	}

	encoder := func() *utils.MockEncoder {
		return &utils.MockEncoder{
			SetIndentFunc:     func(_ string, _ string) {},
			SetEscapeHTMLFunc: func(_ bool) {},
			EncodeFunc:        func(_ any) error { return nil },
		}
	}

	encoderFactory := func() *utils.MockEncoderFactory {
		return &utils.MockEncoderFactory{
			EncoderFunc: func(_ io.Writer) utils.Encoder {
				return encoder()
			},
		}
	}

	t.Run("When no error save will succeed", func(t *testing.T) {
		// given
		mockEncoder := encoder()
		mockEncoderFactory := &utils.MockEncoderFactory{
			EncoderFunc: func(_ io.Writer) utils.Encoder {
				return mockEncoder
			},
		}
		mockOsProxy := osProxy()

		// when
		config := DefaultConfig{
			ProxyVersion:           "1.0.0",
			WrapperVersion:         "1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild",
			BuildCacheEnabled:      true,
		}
		err := config.Save(mockOsProxy, mockEncoderFactory)

		// then
		require.NoError(t, err)

		require.Len(t, mockOsProxy.MkdirAllCalls(), 1)
		assert.Equal(t, "~/.bitrise-xcelerate", mockOsProxy.MkdirAllCalls()[0].S)
		require.Len(t, mockOsProxy.CreateCalls(), 1)
		assert.Equal(t, "~/.bitrise-xcelerate/config.json", mockOsProxy.CreateCalls()[0].S)
		require.Len(t, mockEncoder.SetIndentCalls(), 1)
		assert.Empty(t, mockEncoder.SetIndentCalls()[0].Prefix)
		assert.Equal(t, "  ", mockEncoder.SetIndentCalls()[0].Indent)
		require.Len(t, mockEncoder.SetEscapeHTMLCalls(), 1)
		assert.False(t, mockEncoder.SetEscapeHTMLCalls()[0].Escape)
		require.Len(t, mockEncoder.EncodeCalls(), 1)
		assert.Equal(t, mockEncoder.EncodeCalls()[0].Data, &config)
	})

	t.Run("When error occurs when getting user home save returns an error", func(t *testing.T) {
		// given
		mockOsProxy := &utils.MockOsProxy{
			UserHomeDirFunc: func() (string, error) {
				return "", os.ErrNotExist
			},
		}

		// when
		config := DefaultConfig{
			ProxyVersion:           "1.0.0",
			WrapperVersion:         "1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild",
			BuildCacheEnabled:      true,
		}
		err := config.Save(mockOsProxy, encoderFactory())

		// then
		assert.EqualError(t, err, fmt.Errorf(errFmtDetermineHome, os.ErrNotExist).Error())
	})

	t.Run("When error occurs making directories save returns an error", func(t *testing.T) {
		// given
		mockOsProxy := &utils.MockOsProxy{
			UserHomeDirFunc: func() (string, error) {
				return "~", nil
			},
			MkdirAllFunc: func(path string, perm os.FileMode) error {
				return os.ErrNotExist
			},
		}

		// when
		config := DefaultConfig{
			ProxyVersion:           "1.0.0",
			WrapperVersion:         "1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild",
			BuildCacheEnabled:      true,
		}
		err := config.Save(mockOsProxy, encoderFactory())

		// then
		assert.EqualError(t, err, fmt.Errorf(errFmtCreateFolder, "~/.bitrise-xcelerate", os.ErrNotExist).Error())
	})

	t.Run("When error occurs when creating config file, it returns an error", func(t *testing.T) {
		// given
		mockOsProxy := &utils.MockOsProxy{
			UserHomeDirFunc: func() (string, error) {
				return "~", nil
			},
			MkdirAllFunc: func(path string, perm os.FileMode) error {
				return nil
			},
			CreateFunc: func(path string) (*os.File, error) {
				return nil, os.ErrNotExist
			},
		}

		// when
		config := DefaultConfig{
			ProxyVersion:           "1.0.0",
			WrapperVersion:         "1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild",
			BuildCacheEnabled:      true,
		}
		err := config.Save(mockOsProxy, encoderFactory())

		// then
		assert.EqualError(t, err, fmt.Errorf(errFmtCreateConfigFile, os.ErrNotExist).Error())
	})

	t.Run("When error occurs when encoding config file, it returns an error", func(t *testing.T) {
		// given
		encodingError := errors.New("encoding error")

		mockEncoder := &utils.MockEncoder{
			SetIndentFunc:     func(_ string, _ string) {},
			SetEscapeHTMLFunc: func(_ bool) {},
			EncodeFunc:        func(_ any) error { return encodingError },
		}

		mockEncoderFactory := &utils.MockEncoderFactory{
			EncoderFunc: func(_ io.Writer) utils.Encoder {
				return mockEncoder
			},
		}

		// when
		config := DefaultConfig{
			ProxyVersion:           "1.0.0",
			WrapperVersion:         "1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild",
			BuildCacheEnabled:      true,
		}
		err := config.Save(osProxy(), mockEncoderFactory)

		// then
		assert.EqualError(t, err, fmt.Errorf(errFmtEncodeConfigFile, encodingError).Error())
	})
}
