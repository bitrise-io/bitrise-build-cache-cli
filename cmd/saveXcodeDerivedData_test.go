package cmd

import (
	"github.com/stretchr/testify/mock"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/require"
)

func Test_saveXcodeDerivedDataCmdFn(t *testing.T) {
	// given
	prep := func() log.Logger {
		mockLogger := &mocks.Logger{}
		mockLogger.On("Infof", mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()

		return mockLogger
	}

	// when
	t.Run("No envs specified", func(t *testing.T) {
		mockLogger := prep()
		envVars := createEnvProvider(map[string]string{})
		err := saveXcodeDerivedDataCmdFn(mockLogger, envVars)

		// then
		require.EqualError(t, err, "read auth config from environments: AuthToken not provided")
	})

	t.Run("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN specified", func(t *testing.T) {
		mockLogger := prep()
		envVars := createEnvProvider(map[string]string{
			"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": "ServiceAccessTokenValue",
		})
		err := saveXcodeDerivedDataCmdFn(mockLogger, envVars)

		// then
		require.EqualError(t, err, "cache key is required if BITRISE_GIT_BRANCH is not set")
	})
}
