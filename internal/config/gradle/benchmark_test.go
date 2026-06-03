//nolint:maintidx
package gradleconfig

import (
	"fmt"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	commonmocks "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common/mocks"
)

type noopExporter struct{}

func (n *noopExporter) Export(_, _ string)          {}
func (n *noopExporter) ExportToShellRC(_, _ string) {}

func Test_ApplyBenchmarkPhase(t *testing.T) {
	prep := func() log.Logger {
		mockLogger := &mocks.Logger{}
		mockLogger.On("Infof", mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything, mock.Anything).Return()
		mockLogger.On("Debugf", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
		mockLogger.On("Warnf", mock.Anything).Return()
		mockLogger.On("Warnf", mock.Anything, mock.Anything).Return()

		return mockLogger
	}

	t.Run("baseline phase disables cache and enables analytics", func(t *testing.T) {
		logger := prep()
		params := ActivateGradleParams{
			Cache: CacheParams{
				Enabled:        true,
				JustDependency: true,
				PushEnabled:    true,
			},
			Analytics: AnalyticsParams{
				Enabled: false,
			},
		}

		mockProvider := &commonmocks.BenchmarkPhaseProviderMock{
			GetBenchmarkPhaseFunc: func(_ string, _ common.CacheConfigMetadata) (string, error) {
				return common.BenchmarkPhaseBaseline, nil
			},
		}

		ApplyBenchmarkPhase(&params, logger, mockProvider, common.CacheConfigMetadata{}, &noopExporter{})

		assert.False(t, params.Cache.Enabled)
		assert.False(t, params.Cache.JustDependency)
		assert.True(t, params.Analytics.Enabled)
		assert.Len(t, mockProvider.GetBenchmarkPhaseCalls(), 1)
	})

	t.Run("warmup phase does not change params", func(t *testing.T) {
		logger := prep()
		params := ActivateGradleParams{
			Cache: CacheParams{
				Enabled:     true,
				PushEnabled: true,
			},
			Analytics: AnalyticsParams{
				Enabled: false,
			},
		}

		mockProvider := &commonmocks.BenchmarkPhaseProviderMock{
			GetBenchmarkPhaseFunc: func(_ string, _ common.CacheConfigMetadata) (string, error) {
				return common.BenchmarkPhaseWarmup, nil
			},
		}

		ApplyBenchmarkPhase(&params, logger, mockProvider, common.CacheConfigMetadata{}, &noopExporter{})

		assert.True(t, params.Cache.Enabled)
		assert.True(t, params.Cache.PushEnabled)
		assert.False(t, params.Analytics.Enabled)
	})

	t.Run("empty phase does not change params", func(t *testing.T) {
		logger := prep()
		params := ActivateGradleParams{
			Cache: CacheParams{
				Enabled: true,
			},
			Analytics: AnalyticsParams{
				Enabled: false,
			},
		}

		mockProvider := &commonmocks.BenchmarkPhaseProviderMock{
			GetBenchmarkPhaseFunc: func(_ string, _ common.CacheConfigMetadata) (string, error) {
				return "", nil
			},
		}

		ApplyBenchmarkPhase(&params, logger, mockProvider, common.CacheConfigMetadata{}, &noopExporter{})

		assert.True(t, params.Cache.Enabled)
		assert.False(t, params.Analytics.Enabled)
	})

	t.Run("error falls back to original params", func(t *testing.T) {
		logger := prep()
		params := ActivateGradleParams{
			Cache: CacheParams{
				Enabled: true,
			},
			Analytics: AnalyticsParams{
				Enabled: false,
			},
		}

		mockProvider := &commonmocks.BenchmarkPhaseProviderMock{
			GetBenchmarkPhaseFunc: func(_ string, _ common.CacheConfigMetadata) (string, error) {
				return "", fmt.Errorf("network error")
			},
		}

		ApplyBenchmarkPhase(&params, logger, mockProvider, common.CacheConfigMetadata{}, &noopExporter{})

		assert.True(t, params.Cache.Enabled)
		assert.False(t, params.Analytics.Enabled)
	})
}
