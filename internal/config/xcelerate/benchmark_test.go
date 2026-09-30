//go:build unit

package xcelerate_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	commonmocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common/mocks"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
)

type noopExporter struct{}

func (n *noopExporter) Export(_, _ string) {}

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

// The provider hands back an override indistinguishable from an API answer, and
// ResolveBenchmarkPhase refuses to record one for a reason activation shares: a
// recorded pin outlives the build that set it and keeps the cache off.
func TestApplyBenchmarkPhase_AnOverrideIsAppliedButNotRecorded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(common.BenchmarkPhaseEnvVar(common.BuildToolXcode), common.BenchmarkPhaseBaseline)

	params := xcelerate.Params{BuildCacheEnabled: true}
	provider := &commonmocks.BenchmarkPhaseProviderMock{
		GetBenchmarkPhaseFunc: func(_ string, _ common.CacheConfigMetadata) (string, error) {
			return common.BenchmarkPhaseBaseline, nil
		},
	}

	xcelerate.ApplyBenchmarkPhase(&params, mockLogger, provider, common.CacheConfigMetadata{BitriseBuildID: "build-1"}, &noopExporter{})

	assert.False(t, params.BuildCacheEnabled, "the override still decides this build")
	assert.Empty(t, common.ReadBenchmarkPhaseFile(common.BuildToolXcode, mockLogger))
}

func TestApplyBenchmarkPhase_AnAPIAnswerIsRecorded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(common.BenchmarkPhaseEnvVar(common.BuildToolXcode), "")

	params := xcelerate.Params{BuildCacheEnabled: true}
	provider := &commonmocks.BenchmarkPhaseProviderMock{
		GetBenchmarkPhaseFunc: func(_ string, _ common.CacheConfigMetadata) (string, error) {
			return common.BenchmarkPhaseWarmup, nil
		},
	}

	xcelerate.ApplyBenchmarkPhase(&params, mockLogger, provider, common.CacheConfigMetadata{BitriseBuildID: "build-1"}, &noopExporter{})

	assert.Equal(t, common.BenchmarkPhaseWarmup, common.ReadBenchmarkPhaseFile(common.BuildToolXcode, mockLogger))
}
