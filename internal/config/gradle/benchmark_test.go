//nolint:maintidx
package gradleconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	commonmocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/envexport"
)

type noopExporter struct{}

func (n *noopExporter) Export(_, _ string) {}

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
		assert.False(t, params.Cache.PushEnabled)
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

	// A phase in a shell RC file outlives the build that produced it, and
	// GetBenchmarkPhase short-circuits on the env var before calling the API — so
	// one baseline result would pin the phase and keep the cache off for good.
	t.Run("does not touch shell rc files", func(t *testing.T) {
		logger := prep()
		home := t.TempDir()
		t.Setenv("HOME", home)

		params := ActivateGradleParams{Cache: CacheParams{Enabled: true}}
		mockProvider := &commonmocks.BenchmarkPhaseProviderMock{
			GetBenchmarkPhaseFunc: func(_ string, _ common.CacheConfigMetadata) (string, error) {
				return common.BenchmarkPhaseBaseline, nil
			},
		}

		ApplyBenchmarkPhase(&params, logger, mockProvider, common.CacheConfigMetadata{}, envexport.New(map[string]string{"HOME": home}, logger))

		for _, rc := range []string{".bashrc", ".zshrc", ".profile", ".zprofile"} {
			_, err := os.Stat(filepath.Join(home, rc))
			assert.True(t, os.IsNotExist(err), "%s must not be written by benchmark phasing", rc)
		}
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
