package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/xcode"
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

	t.Run("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN specified", func(t *testing.T) {
		mockLogger, mockTracker := prep()
		envVars := createEnvProvider(map[string]string{
			"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": "ServiceAccessTokenValue",
		})
		_, err := saveXcodeDerivedDataFilesCmdFn(context.Background(), common.CacheAuthConfig{}, "", "", "", "", "", false, mockLogger, mockTracker, time.Now(), envVars)

		// then
		require.EqualError(t, err, "get cache key: cache key is required if BITRISE_GIT_BRANCH env var is not set")
	})
}
