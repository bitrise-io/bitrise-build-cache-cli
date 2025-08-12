package xcelerate

import (
	"errors"
	"fmt"
	"os"
	"testing"

	utilMocks "github.com/bitrise-io/bitrise-build-cache-cli/internal/utils/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_activateXcodeCmdFn(t *testing.T) {
	t.Run("When no error save will succeed", func(t *testing.T) {
		// given
		mockOsProxy := &utilMocks.MockOsProxy{}
		mockOsProxy.On("UserHomeDir").Return("~", nil)
		mockOsProxy.On("MkdirAll", mock.Anything, mock.Anything).Return(nil)
		mockOsProxy.On("Create", mock.Anything).Return(&os.File{}, nil)

		mockEncoder := &utilMocks.MockEncoder{}
		mockEncoder.On("SetIndent", mock.Anything, mock.Anything).Return()
		mockEncoder.On("SetEscapeHTML", mock.Anything).Return()
		mockEncoder.On("Encode", mock.Anything).Return(nil)

		mockEncoderFactory := &utilMocks.MockEncoderFactory{}
		mockEncoderFactory.On("Encoder").Return(mockEncoder)

		// when
		config := DefaultConfig{
			ProxyVersion:           "1.0.0",
			WrapperVersion:         "1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild",
			BuildCacheEnabled:      true,
		}
		err := config.Save(mockOsProxy, mockEncoderFactory)

		// then
		mockOsProxy.AssertCalled(t, "MkdirAll", "~/.bitrise-xcelerate", mock.Anything)
		mockOsProxy.AssertCalled(t, "Create", "~/.bitrise-xcelerate/config.json")
		mockEncoder.AssertCalled(t, "SetIndent", "", "  ")
		mockEncoder.AssertCalled(t, "SetEscapeHTML", false)
		mockEncoder.AssertCalled(t, "Encode", &config)
		require.NoError(t, err)
	})

	t.Run("When error occurs when getting user home save returns an error", func(t *testing.T) {
		// given
		mockOsProxy := &utilMocks.MockOsProxy{}
		mockOsProxy.On("UserHomeDir").Return("", os.ErrNotExist)
		mockOsProxy.On("MkdirAll", mock.Anything, mock.Anything).Return(nil)
		mockOsProxy.On("Create", mock.Anything).Return(&os.File{}, nil)

		mockEncoder := &utilMocks.MockEncoder{}
		mockEncoder.On("SetIndent", mock.Anything, mock.Anything).Return()
		mockEncoder.On("SetEscapeHTML", mock.Anything).Return()
		mockEncoder.On("Encode", mock.Anything).Return(nil)

		mockEncoderFactory := &utilMocks.MockEncoderFactory{}
		mockEncoderFactory.On("Encoder").Return(mockEncoder)

		// when
		config := DefaultConfig{
			ProxyVersion:           "1.0.0",
			WrapperVersion:         "1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild",
			BuildCacheEnabled:      true,
		}
		err := config.Save(mockOsProxy, mockEncoderFactory)

		// then
		require.EqualError(t, err, fmt.Errorf(errFmtDetermineHome, os.ErrNotExist).Error())
	})

	t.Run("When error occurs making directories save returns an error", func(t *testing.T) {
		// given
		mockOsProxy := &utilMocks.MockOsProxy{}
		mockOsProxy.On("UserHomeDir").Return("~", nil)
		mockOsProxy.On("MkdirAll", mock.Anything, mock.Anything).Return(os.ErrNotExist)
		mockOsProxy.On("Create", mock.Anything).Return(&os.File{}, nil)

		mockEncoder := &utilMocks.MockEncoder{}
		mockEncoder.On("SetIndent", mock.Anything, mock.Anything).Return()
		mockEncoder.On("SetEscapeHTML", mock.Anything).Return()
		mockEncoder.On("Encode", mock.Anything).Return(nil)

		mockEncoderFactory := &utilMocks.MockEncoderFactory{}
		mockEncoderFactory.On("Encoder").Return(mockEncoder)

		// when
		config := DefaultConfig{
			ProxyVersion:           "1.0.0",
			WrapperVersion:         "1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild",
			BuildCacheEnabled:      true,
		}
		err := config.Save(mockOsProxy, mockEncoderFactory)

		// then
		require.EqualError(t, err, fmt.Errorf(errFmtCreateFolder, "~/.bitrise-xcelerate", os.ErrNotExist).Error())
	})

	t.Run("When error occurs when creating config file, it returns an error", func(t *testing.T) {
		// given
		mockOsProxy := &utilMocks.MockOsProxy{}
		mockOsProxy.On("UserHomeDir").Return("~", nil)
		mockOsProxy.On("MkdirAll", mock.Anything, mock.Anything).Return(nil)
		mockOsProxy.On("Create", mock.Anything).Return(nil, os.ErrNotExist)

		mockEncoder := &utilMocks.MockEncoder{}
		mockEncoder.On("SetIndent", mock.Anything, mock.Anything).Return()
		mockEncoder.On("SetEscapeHTML", mock.Anything).Return()
		mockEncoder.On("Encode", mock.Anything).Return(nil)

		mockEncoderFactory := &utilMocks.MockEncoderFactory{}
		mockEncoderFactory.On("Encoder").Return(mockEncoder)

		// when
		config := DefaultConfig{
			ProxyVersion:           "1.0.0",
			WrapperVersion:         "1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild",
			BuildCacheEnabled:      true,
		}
		err := config.Save(mockOsProxy, mockEncoderFactory)

		// then
		require.EqualError(t, err, fmt.Errorf(errFmtCreateConfigFile, os.ErrNotExist).Error())
	})

	t.Run("When error occurs when encoding config file, it returns an error", func(t *testing.T) {
		// given
		encodingError := errors.New("encoding error")

		mockOsProxy := &utilMocks.MockOsProxy{}
		mockOsProxy.On("UserHomeDir").Return("~", nil)
		mockOsProxy.On("MkdirAll", mock.Anything, mock.Anything).Return(nil)
		mockOsProxy.On("Create", mock.Anything).Return(&os.File{}, nil)

		mockEncoder := &utilMocks.MockEncoder{}
		mockEncoder.On("SetIndent", mock.Anything, mock.Anything).Return()
		mockEncoder.On("SetEscapeHTML", mock.Anything).Return()
		mockEncoder.On("Encode", mock.Anything).Return(encodingError)

		mockEncoderFactory := &utilMocks.MockEncoderFactory{}
		mockEncoderFactory.On("Encoder").Return(mockEncoder)

		// when
		config := DefaultConfig{
			ProxyVersion:           "1.0.0",
			WrapperVersion:         "1.0.0",
			OriginalXcodebuildPath: "/usr/bin/xcodebuild",
			BuildCacheEnabled:      true,
		}
		err := config.Save(mockOsProxy, mockEncoderFactory)

		// then
		require.EqualError(t, err, fmt.Errorf(errFmtEncodeConfigFile, encodingError).Error())
	})
}
