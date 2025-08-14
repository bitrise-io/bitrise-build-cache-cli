//nolint:dupl
package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/internal/utils/mocks"
	goUtilsMocks "github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_activateXcodeCmdFn(t *testing.T) {
	logger := func() *goUtilsMocks.Logger {
		mockLogger := &goUtilsMocks.Logger{}
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

	config := func() *mocks.ConfigMock {
		return &mocks.ConfigMock{
			SaveFunc: func(_ utils.OsProxy, _ utils.EncoderFactory) error {
				return nil
			},
		}
	}

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

	t.Run("When no error activateXcodeCmdFn logs success", func(t *testing.T) {
		mockLogger := logger()

		err := activateXcodeCommandFn(
			mockLogger,
			osProxy(),
			encoderFactory(),
			config(),
		)

		mockLogger.AssertCalled(t, "TInfof", activateXcodeSuccessful)
		require.NoError(t, err)
	})

	t.Run("When config save returns error activateXcodeCmdFn fails", func(t *testing.T) {
		expectedError := errors.New("failed to save config")

		mockConfig := &mocks.ConfigMock{
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
