package cmd

import (
	"testing"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcode"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"context"

	xcodeMocks "github.com/bitrise-io/bitrise-build-cache-cli/internal/xcode/mocks"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
)

func Test_saveXcodeDerivedDataFilesCmdFn(t *testing.T) {
	// given
	prep := func() (log.Logger, xcode.StepAnalyticsTracker) {
		mockLogger := &mocks.Logger{}
		mockLogger.On("Infof", mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
		mockLogger.On("TInfof", mock.Anything).Return()
		mockLogger.On("TInfof", mock.Anything, mock.Anything).Return()

		mockTracker := &xcodeMocks.StepAnalyticsTrackerMock{}

		return mockLogger, mockTracker
	}

	// when
	t.Run("No envs specified", func(t *testing.T) {
		mockLogger, mockTracker := prep()
		envVars := createEnvProvider(map[string]string{})
		err := saveXcodeDerivedDataFilesCmdFn(context.Background(), "", "", ".", "some-key", "", mockLogger, mockTracker, time.Now(), envVars)

		// then
		require.EqualError(t, err, "read auth config from environments: BITRISE_BUILD_CACHE_AUTH_TOKEN or BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN environment variable not set")
	})

	t.Run("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN specified", func(t *testing.T) {
		mockLogger, mockTracker := prep()
		envVars := createEnvProvider(map[string]string{
			"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": "ServiceAccessTokenValue",
		})
		err := saveXcodeDerivedDataFilesCmdFn(context.Background(), "", "", "", "", "", mockLogger, mockTracker, time.Now(), envVars)

		// then
		require.EqualError(t, err, "get cache key: cache key is required if BITRISE_GIT_BRANCH env var is not set")
	})
}
