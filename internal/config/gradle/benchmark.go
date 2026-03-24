package gradleconfig

import (
	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
)

// EnvExporter abstracts environment variable export for testability.
type EnvExporter interface {
	Export(key, value string)
	ExportToShellRC(blockName, content string)
}

// ApplyBenchmarkPhase queries the benchmark phase and overrides gradle params accordingly.
// Baseline phase disables cache and enables analytics only. Warmup phase logs a warning.
// The phase is exported as BITRISE_BUILD_CACHE_BENCHMARK_PHASE env var
// and written to ~/.local/state/xcelerate/benchmark/benchmark-phase.json as fallback.
func ApplyBenchmarkPhase(
	params *ActivateGradleParams,
	logger log.Logger,
	benchmarkProvider common.BenchmarkPhaseProvider,
	metadata common.CacheConfigMetadata,
	exporter EnvExporter,
) {
	phase, err := benchmarkProvider.GetBenchmarkPhase(common.BuildToolGradle, metadata)
	if err != nil {
		logger.Debugf("Failed to fetch benchmark phase, using configured flags: %v", err)

		return
	}

	if phase == "" {
		logger.Debugf("No benchmark phase found, using configured flags")

		return
	}

	logger.Infof("(i) Benchmark phase: %s", phase)
	exporter.Export("BITRISE_BUILD_CACHE_BENCHMARK_PHASE", phase)
	exporter.ExportToShellRC("Bitrise Benchmark Phase", "export BITRISE_BUILD_CACHE_BENCHMARK_PHASE="+phase)
	common.WriteBenchmarkPhaseFile(phase, logger)

	switch phase {
	case common.BenchmarkPhaseBaseline:
		logger.Warnf("Benchmark baseline mode: disabling cache and enabling analytics only")
		params.Cache.Enabled = false
		params.Cache.JustDependency = false
		params.Analytics.Enabled = true
	case common.BenchmarkPhaseWarmup:
		logger.Infof("(i) Benchmark warmup phase: cache performance might not be ideal")
	}
}
