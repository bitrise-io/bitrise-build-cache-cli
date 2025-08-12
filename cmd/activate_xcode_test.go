//nolint:dupl
package cmd

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	utilMocks "github.com/bitrise-io/bitrise-build-cache-cli/internal/utils/mocks"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_activateXcodeCmdFn(t *testing.T) {
	mockLogger := func() log.Logger {
		mockLogger := &mocks.Logger{}
		mockLogger.On("Infof", mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()

		return mockLogger
	}

	t.Run("When no error activateXcodeCmdFn writes the xcode config file", func(t *testing.T) {
		var configFilePath string

		err := activateXcodeCommandFn(
			mockLogger(),
			utilMocks.MockOsProxy{
				UserHomeDir: func() (string, error) { return "/home/user", nil },
				MkdirAll:    func(string, os.FileMode) error { return nil },
				Create: func(input string) (*os.File, error) {
					configFilePath = input
					return nil, nil
				},
			}.Proxy(),
			utilMocks.MockEncoderFactory{
				Mock: utilMocks.MockEncoder{
					MockEncode: func(any) error {
						return nil
					},
				},
			},
		)

		require.Equal(t, "/home/user/.bitrise-xcelerate/config.json", configFilePath)
		require.NoError(t, err)
	})

	t.Run("When error occurs when getting user home, it returns an error", func(t *testing.T) {
		err := activateXcodeCommandFn(
			mockLogger(),
			utilMocks.MockOsProxy{
				UserHomeDir: func() (string, error) { return "", os.ErrNotExist },
				MkdirAll:    func(string, os.FileMode) error { return nil },
				Create:      func(string) (*os.File, error) { return nil, nil },
			}.Proxy(),
			utilMocks.MockEncoderFactory{
				Mock: utilMocks.MockEncoder{
					MockEncode: func(any) error {
						return nil
					},
				},
			},
		)

		require.Error(t, err)
		require.EqualError(t, err, fmt.Errorf(errFmtCreateXcodeConfig, fmt.Errorf(xcelerate.ErrFmtDetermineHome, os.ErrNotExist)).Error())
	})

	t.Run("When error occurs when making directories, it returns an error", func(t *testing.T) {
		var mkdirPath string

		err := activateXcodeCommandFn(
			mockLogger(),
			utilMocks.MockOsProxy{
				UserHomeDir: func() (string, error) { return "/home/user", nil },
				MkdirAll: func(path string, _ os.FileMode) error {
					mkdirPath = path
					return os.ErrNotExist
				},
				Create: func(string) (*os.File, error) { return nil, nil },
			}.Proxy(),
			utilMocks.MockEncoderFactory{
				Mock: utilMocks.MockEncoder{
					MockEncode: func(any) error {
						return nil
					},
				},
			},
		)

		require.Error(t, err)
		require.EqualError(t, err, fmt.Errorf(errFmtCreateXcodeConfig, fmt.Errorf(xcelerate.ErrFmtCreateFolder, mkdirPath, os.ErrNotExist)).Error())
	})

	t.Run("When error occurs when creating config file, it returns an error", func(t *testing.T) {
		err := activateXcodeCommandFn(
			mockLogger(),
			utilMocks.MockOsProxy{
				UserHomeDir: func() (string, error) { return "/home/user", nil },
				MkdirAll:    func(string, os.FileMode) error { return nil },
				Create:      func(string) (*os.File, error) { return nil, os.ErrNotExist },
			}.Proxy(),
			utilMocks.MockEncoderFactory{
				Mock: utilMocks.MockEncoder{
					MockEncode: func(any) error {
						return nil
					},
				},
			},
		)

		require.Error(t, err)
		require.EqualError(t, err, fmt.Errorf(errFmtCreateXcodeConfig, fmt.Errorf(xcelerate.ErrFmtCreateConfigFile, os.ErrNotExist)).Error())
	})

	t.Run("When error occurs when encoding config file, it returns an error", func(t *testing.T) {
		encodingError := errors.New("encoding error")

		err := activateXcodeCommandFn(
			mockLogger(),
			utilMocks.MockOsProxy{
				UserHomeDir: func() (string, error) { return "/home/user", nil },
				MkdirAll:    func(string, os.FileMode) error { return nil },
				Create:      func(string) (*os.File, error) { return nil, nil },
			}.Proxy(),
			utilMocks.MockEncoderFactory{
				Mock: utilMocks.MockEncoder{
					MockEncode: func(any) error {
						return encodingError
					},
				},
			},
		)

		require.Error(t, err)
		require.EqualError(t, err, fmt.Errorf(errFmtCreateXcodeConfig, fmt.Errorf(xcelerate.ErrFmtEncodeConfigFile, encodingError)).Error())
	})
}
