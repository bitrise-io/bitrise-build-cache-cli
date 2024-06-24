package cmd

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
)

func Test_saveXcodeDerivedDataCmdFn(t *testing.T) {
	// given
	prep := func() log.Logger {
		mockLogger := &mocks.Logger{}
		mockLogger.On("Infof", mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
		mockLogger.On("TInfof", mock.Anything).Return()
		mockLogger.On("TInfof", mock.Anything, mock.Anything).Return()

		return mockLogger
	}

	// when
	t.Run("No envs specified", func(t *testing.T) {
		mockLogger := prep()
		envVars := createEnvProvider(map[string]string{})
		err := saveXcodeDerivedDataCmdFn("", "", ".", "some-key", "", mockLogger, envVars)

		// then
		require.EqualError(t, err, "read auth config from environments: AuthToken not provided")
	})

	t.Run("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN specified", func(t *testing.T) {
		mockLogger := prep()
		envVars := createEnvProvider(map[string]string{
			"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": "ServiceAccessTokenValue",
		})
		err := saveXcodeDerivedDataCmdFn("", "", ".", "", "", mockLogger, envVars)

		// then
		require.EqualError(t, err, "get cache key: cache key is required if BITRISE_GIT_BRANCH env var is not set")
	})
}
