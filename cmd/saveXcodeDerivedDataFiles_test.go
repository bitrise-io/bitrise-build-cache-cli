package cmd_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	xcodeMocks "github.com/bitrise-io/bitrise-build-cache-cli/internal/xcode/mocks"
)

func Test_saveXcodeDerivedDataFilesCmdFn(t *testing.T) {
	// given
	mockTracker := &xcodeMocks.StepAnalyticsTrackerMock{}

	t.Run("BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN specified", func(t *testing.T) {
		envVars := map[string]string{
			"BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN": "ServiceAccessTokenValue",
		}
		command := func(_ string, _ ...string) (string, error) {
			return "", nil
		}
		_, err := cmd.SaveXcodeDerivedDataFilesCmdFn(context.Background(), common.CacheAuthConfig{}, "", "", "", "", "", false, false, mockLogger, mockTracker, time.Now(), envVars, command)

		// then
		require.EqualError(t, err, "get cache key: cache key is required if BITRISE_GIT_BRANCH env var is not set")
	})
}
