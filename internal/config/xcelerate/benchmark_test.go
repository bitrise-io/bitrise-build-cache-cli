//go:build unit

package xcelerate_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	commonmocks "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/xcelerate"
)

type noopExporter struct{}

func (n *noopExporter) Export(_, _ string)          {}
func (n *noopExporter) ExportToShellRC(_, _ string) {}

func TestApplyBenchmarkPhase(t *testing.T) {
	t.Run("baseline phase disables cache", func(t *testing.T) {
		params := xcelerate.Params{
			BuildCacheEnabled: true,
			PushEnabled:       true,
		}

		mockProvider := &commonmocks.BenchmarkPhaseProviderMock{
			GetBenchmarkPhaseFunc: func(buildTool string, _ common.CacheConfigMetadata) (string, error) {
				assert.Equal(t, common.BuildToolXcode, buildTool)

				return common.BenchmarkPhaseBaseline, nil
			},
		}

		xcelerate.ApplyBenchmarkPhase(&params, mockLogger, mockProvider, common.CacheConfigMetadata{}, &noopExporter{})

		assert.False(t, params.BuildCacheEnabled)
		assert.Len(t, mockProvider.GetBenchmarkPhaseCalls(), 1)
	})

	t.Run("warmup phase does not change params", func(t *testing.T) {
		params := xcelerate.Params{
			BuildCacheEnabled: true,
			PushEnabled:       true,
		}

		mockProvider := &commonmocks.BenchmarkPhaseProviderMock{
			GetBenchmarkPhaseFunc: func(_ string, _ common.CacheConfigMetadata) (string, error) {
				return common.BenchmarkPhaseWarmup, nil
			},
		}

		xcelerate.ApplyBenchmarkPhase(&params, mockLogger, mockProvider, common.CacheConfigMetadata{}, &noopExporter{})

		assert.True(t, params.BuildCacheEnabled)
		assert.True(t, params.PushEnabled)
	})

	t.Run("empty phase does not change params", func(t *testing.T) {
		params := xcelerate.Params{
			BuildCacheEnabled: true,
		}

		mockProvider := &commonmocks.BenchmarkPhaseProviderMock{
			GetBenchmarkPhaseFunc: func(_ string, _ common.CacheConfigMetadata) (string, error) {
				return "", nil
			},
		}

		xcelerate.ApplyBenchmarkPhase(&params, mockLogger, mockProvider, common.CacheConfigMetadata{}, &noopExporter{})

		assert.True(t, params.BuildCacheEnabled)
	})

	t.Run("error falls back to original params", func(t *testing.T) {
		params := xcelerate.Params{
			BuildCacheEnabled: true,
		}

		mockProvider := &commonmocks.BenchmarkPhaseProviderMock{
			GetBenchmarkPhaseFunc: func(_ string, _ common.CacheConfigMetadata) (string, error) {
				return "", fmt.Errorf("network error")
			},
		}

		xcelerate.ApplyBenchmarkPhase(&params, mockLogger, mockProvider, common.CacheConfigMetadata{}, &noopExporter{})

		assert.True(t, params.BuildCacheEnabled)
	})
}
