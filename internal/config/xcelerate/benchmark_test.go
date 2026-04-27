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

type recordingShellRCExporter struct {
	blocks map[string]string
}

func (r *recordingShellRCExporter) Export(_, _ string) {}
func (r *recordingShellRCExporter) ExportToShellRC(blockName, content string) {
	if r.blocks == nil {
		r.blocks = map[string]string{}
	}
	r.blocks[blockName] = content
}

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

	t.Run("uses xcode-specific shell-rc block name", func(t *testing.T) {
		params := xcelerate.Params{BuildCacheEnabled: true}

		mockProvider := &commonmocks.BenchmarkPhaseProviderMock{
			GetBenchmarkPhaseFunc: func(_ string, _ common.CacheConfigMetadata) (string, error) {
				return common.BenchmarkPhaseWarmup, nil
			},
		}

		exporter := &recordingShellRCExporter{}
		xcelerate.ApplyBenchmarkPhase(&params, mockLogger, mockProvider, common.CacheConfigMetadata{}, exporter)

		_, hasXcodeBlock := exporter.blocks["Bitrise Benchmark Phase Xcode"]
		assert.True(t, hasXcodeBlock, "expected xcode-specific block name so gradle activation doesn't clobber it")
		_, hasGenericBlock := exporter.blocks["Bitrise Benchmark Phase"]
		assert.False(t, hasGenericBlock, "generic block name reintroduces the cross-tool clobber")
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
