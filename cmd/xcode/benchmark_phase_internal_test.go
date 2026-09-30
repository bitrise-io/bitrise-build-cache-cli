//go:build unit

package xcode

import (
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
)

func neverResolves(t *testing.T) func(xcelerate.Config, configcommon.CacheConfigMetadata, log.Logger) string {
	t.Helper()

	return func(xcelerate.Config, configcommon.CacheConfigMetadata, log.Logger) string {
		t.Fatal("the phase must not be resolved for this invocation")

		return ""
	}
}

// A fastlane or CocoaPods run fires dozens of -version / -showBuildSettings
// calls. The phase cannot change any of them, and a benchmark API that is down
// would cost each one the client's retries against a 10s timeout.
func TestApplyBenchmarkPhase_AQueryInvocationAsksNothing(t *testing.T) {
	phase, cacheEnabled := applyBenchmarkPhase(false, xcelerate.Config{BuildCacheEnabled: true},
		configcommon.CacheConfigMetadata{}, log.NewLogger(), neverResolves(t))

	assert.Empty(t, phase)
	assert.True(t, cacheEnabled)
}

// With the cache already off — --no-bitrise-build-cache, -create-xcframework,
// project mode — there is nothing left for a baseline to disable.
func TestApplyBenchmarkPhase_ADisabledCacheAsksNothing(t *testing.T) {
	phase, cacheEnabled := applyBenchmarkPhase(true, xcelerate.Config{BuildCacheEnabled: false},
		configcommon.CacheConfigMetadata{}, log.NewLogger(), neverResolves(t))

	assert.Empty(t, phase)
	assert.False(t, cacheEnabled)
}

// Baseline measures the build without the cache, so the cache goes off before
// the proxy would have been started.
func TestApplyBenchmarkPhase_BaselineTurnsTheCacheOff(t *testing.T) {
	phase, cacheEnabled := applyBenchmarkPhase(true, xcelerate.Config{BuildCacheEnabled: true},
		configcommon.CacheConfigMetadata{}, log.NewLogger(),
		func(xcelerate.Config, configcommon.CacheConfigMetadata, log.Logger) string {
			return configcommon.BenchmarkPhaseBaseline
		})

	assert.Equal(t, configcommon.BenchmarkPhaseBaseline, phase)
	assert.False(t, cacheEnabled)
}

func TestApplyBenchmarkPhase_WarmupLeavesTheCacheOn(t *testing.T) {
	phase, cacheEnabled := applyBenchmarkPhase(true, xcelerate.Config{BuildCacheEnabled: true},
		configcommon.CacheConfigMetadata{}, log.NewLogger(),
		func(xcelerate.Config, configcommon.CacheConfigMetadata, log.Logger) string {
			return configcommon.BenchmarkPhaseWarmup
		})

	assert.Equal(t, configcommon.BenchmarkPhaseWarmup, phase)
	assert.True(t, cacheEnabled)
}

// Off CI, or without a workspace, there is nothing to query — but an override
// and this build's own record must still be honoured, and another build's
// record must not be.
func TestResolveBenchmarkPhase_WithoutAProviderReadsTheOverrideAndTheBuildsOwnRecord(t *testing.T) {
	t.Run("override", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv(configcommon.BenchmarkPhaseEnvVar(configcommon.BuildToolXcode), configcommon.BenchmarkPhaseBaseline)

		got := resolveBenchmarkPhase(xcelerate.Config{}, configcommon.CacheConfigMetadata{}, log.NewLogger())

		assert.Equal(t, configcommon.BenchmarkPhaseBaseline, got)
	})

	t.Run("this build's record", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv(configcommon.BenchmarkPhaseEnvVar(configcommon.BuildToolXcode), "")
		meta := configcommon.CacheConfigMetadata{BitriseBuildID: "build-1"}
		configcommon.RecordBenchmarkPhase(configcommon.BuildToolXcode, meta, configcommon.BenchmarkPhaseWarmup, log.NewLogger())

		got := resolveBenchmarkPhase(xcelerate.Config{}, meta, log.NewLogger())

		assert.Equal(t, configcommon.BenchmarkPhaseWarmup, got)
	})

	t.Run("another build's record", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv(configcommon.BenchmarkPhaseEnvVar(configcommon.BuildToolXcode), "")
		configcommon.RecordBenchmarkPhase(configcommon.BuildToolXcode,
			configcommon.CacheConfigMetadata{BitriseBuildID: "build-1"}, configcommon.BenchmarkPhaseBaseline, log.NewLogger())

		got := resolveBenchmarkPhase(xcelerate.Config{}, configcommon.CacheConfigMetadata{BitriseBuildID: "build-2"}, log.NewLogger())

		assert.Empty(t, got)
	})
}
