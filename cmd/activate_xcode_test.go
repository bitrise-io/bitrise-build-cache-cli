//nolint:dupl
package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func Test_activateXcodeCmdFn(t *testing.T) {
	logger := func() *mocks.Logger {
		mockLogger := &mocks.Logger{}
		mockLogger.On("TInfof", mock.Anything).Return()
		mockLogger.On("TInfof", mock.Anything, mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything).Return()
		mockLogger.On("Errorf", mock.Anything, mock.Anything).Return()

		return mockLogger
	}

	config := func() *xcelerate.MockConfig {
		return &xcelerate.MockConfig{
			SaveFunc: func(_ utils.OsProxy, _ utils.EncoderFactory) error {
				return nil
			},
		}
	}

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

	encoderFactory := func() utils.EncoderFactory {
		return &utils.MockEncoderFactory{
			EncoderFunc: func(_ io.Writer) utils.Encoder {
				return encoder()
			},
		}
	}

	t.Run("When no error activateXcodeCmdFn logs success", func(t *testing.T) {
		mockLogger := logger()

		err := activateXcodeCommandFn(
			mockLogger,
			osProxy(),
			encoderFactory(),
			config(),
		)

		mockLogger.AssertCalled(t, "TInfof", activateXcodeSuccessful)
		assert.NoError(t, err)
	})

	t.Run("When config save returns error activateXcodeCmdFn fails", func(t *testing.T) {
		expectedError := errors.New("failed to save config")

		mockConfig := &xcelerate.MockConfig{
			SaveFunc: func(_ utils.OsProxy, _ utils.EncoderFactory) error {
				return expectedError
			},
		}

		err := activateXcodeCommandFn(
			logger(),
			osProxy(),
			encoderFactory(),
			mockConfig,
		)

		assert.ErrorContains(t, err, fmt.Errorf(errFmtCreateXcodeConfig, expectedError).Error())
	})
}
