//nolint:dupl
package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
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

	mockOS := func(
		home func() (string, error),
		mkdir func(path string, perm os.FileMode) error,
		create func(name string) (*os.File, error),
	) utils.OsProxy {
		return utils.OsProxy{
			UserHomeDir: home,
			MkdirAll:    mkdir,
			Create:      create,
		}
	}

	mockEncoder := func(err error) utils.EncoderProxyCreator {
		return func(v io.Writer) utils.EncoderProxy {
			return utils.EncoderProxy{
				Encoder: utils.MockEncoder{
					MockEncode: func(v any) error {
						return err
					},
				},
			}
		}
	}

	t.Run("When no error activateXcodeCmdFn writes the xcode config file", func(t *testing.T) {
		var configFilePath string

		err := activateXcodeCommandFn(
			mockLogger(),
			mockOS(
				func() (string, error) { return "/home/user", nil },
				func(string, os.FileMode) error { return nil },
				func(input string) (*os.File, error) {
					configFilePath = input
					return nil, nil
				},
			),
			mockEncoder(nil),
		)

		require.Equal(t, "/home/user/.bitrise-xcelerate/config.json", configFilePath)
		require.NoError(t, err)
	})

	t.Run("When error occurs when getting user home, it returns an error", func(t *testing.T) {
		err := activateXcodeCommandFn(
			mockLogger(),
			mockOS(
				func() (string, error) { return "", os.ErrNotExist },
				func(string, os.FileMode) error { return nil },
				func(string) (*os.File, error) { return nil, nil },
			),
			mockEncoder(nil),
		)

		require.Error(t, err)
		require.EqualError(t, err, fmt.Errorf(errFmtCreateXcodeConfig, fmt.Errorf(common.ErrFmtDetermineHome, os.ErrNotExist)).Error())
	})

	t.Run("When error occurs when making directories, it returns an error", func(t *testing.T) {
		var mkdirPath string

		err := activateXcodeCommandFn(
			mockLogger(),
			mockOS(
				func() (string, error) { return "/home/user", nil },
				func(path string, _ os.FileMode) error {
					mkdirPath = path
					return os.ErrNotExist
				},
				func(string) (*os.File, error) { return nil, nil },
			),
			mockEncoder(nil),
		)

		require.Error(t, err)
		require.EqualError(t, err, fmt.Errorf(errFmtCreateXcodeConfig, fmt.Errorf(common.ErrFmtCreateFolder, mkdirPath, os.ErrNotExist)).Error())
	})

	t.Run("When error occurs when creating config file, it returns an error", func(t *testing.T) {
		err := activateXcodeCommandFn(
			mockLogger(),
			mockOS(
				func() (string, error) { return "/home/user", nil },
				func(string, os.FileMode) error { return nil },
				func(string) (*os.File, error) { return nil, os.ErrNotExist },
			),
			mockEncoder(nil),
		)

		require.Error(t, err)
		require.EqualError(t, err, fmt.Errorf(errFmtCreateXcodeConfig, fmt.Errorf(common.ErrFmtCreateConfigFile, os.ErrNotExist)).Error())
	})

	t.Run("When error occurs when encoding config file, it returns an error", func(t *testing.T) {
		encodingError := errors.New("encoding error")

		err := activateXcodeCommandFn(
			mockLogger(),
			mockOS(
				func() (string, error) { return "/home/user", nil },
				func(string, os.FileMode) error { return nil },
				func(string) (*os.File, error) { return nil, nil },
			),
			mockEncoder(encodingError),
		)

		require.Error(t, err)
		require.EqualError(t, err, fmt.Errorf(errFmtCreateXcodeConfig, fmt.Errorf(common.ErrFmtEncodeConfigFile, encodingError)).Error())
	})
}
