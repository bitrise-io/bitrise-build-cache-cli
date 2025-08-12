//nolint:dupl
package cmd

import (
	"errors"
	"fmt"
	"os"
	"testing"

	configMocks "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate/mocks"
	utilMocks "github.com/bitrise-io/bitrise-build-cache-cli/internal/utils/mocks"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_activateXcodeCmdFn(t *testing.T) {
	mockLogger := func() *mocks.Logger {
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

	mockOsProxy := func() *utilMocks.MockOsProxy {
		mockOs := &utilMocks.MockOsProxy{}
		mockOs.On("UserHomeDir").Return("/home/user", nil)
		mockOs.On("MkdirAll", mock.Anything, mock.Anything).Return(nil)
		mockOs.On("Create", mock.Anything).Return(&os.File{}, nil)

		return mockOs
	}

	mockEncoder := func() *utilMocks.MockEncoder {
		mockEncoder := &utilMocks.MockEncoder{}
		mockEncoder.On("SetIndent", mock.Anything, mock.Anything).Return()
		mockEncoder.On("SetEscapeHTML", mock.Anything).Return()
		mockEncoder.On("Encode", mock.Anything).Return(nil)

		return mockEncoder
	}

	mockEncoderFactory := func() *utilMocks.MockEncoderFactory {
		mockEncoderFactory := &utilMocks.MockEncoderFactory{}
		mockEncoderFactory.On("Encoder").Return(mockEncoder())

		return mockEncoderFactory
	}

	t.Run("When no error activateXcodeCmdFn logs success", func(t *testing.T) {
		logger := mockLogger()
		mockConfig := &configMocks.MockConfig{}
		mockConfig.On("Save", mock.Anything, mock.Anything).Return(nil)

		err := activateXcodeCommandFn(
			logger,
			mockOsProxy(),
			mockEncoderFactory(),
			mockConfig,
		)

		logger.AssertCalled(t, "TInfof", activateXcodeSuccessful)
		require.NoError(t, err)
	})

	t.Run("When config save returns error activateXcodeCmdFn fails", func(t *testing.T) {
		logger := mockLogger()
		mockConfig := &configMocks.MockConfig{}
		mockConfig.On("Save", mock.Anything, mock.Anything).Return(errors.New("failed to save config"))

		err := activateXcodeCommandFn(
			logger,
			mockOsProxy(),
			mockEncoderFactory(),
			mockConfig,
		)

		require.ErrorContains(t, err, fmt.Errorf(errFmtCreateXcodeConfig, errors.New("failed to save config")).Error())
	})
}
