package xcelerate

import (
	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
)

// EnvExporter abstracts environment variable export for testability.
type EnvExporter interface {
	Export(key, value string)
	ExportToShellRC(blockName, content string)
}

// ApplyBenchmarkPhase queries the benchmark phase and overrides xcode params accordingly.
// Baseline phase disables cache.
// The phase is exported as BITRISE_BUILD_CACHE_BENCHMARK_PHASE_XCODE env var
// and written to ~/.local/state/xcelerate/benchmark/benchmark-phase-xcode.json
// as fallback. The user-facing log line is emitted once by
// common.LogBenchmarkSummary at the end of activation, not from this function.
func ApplyBenchmarkPhase(
	params *Params,
	logger log.Logger,
	benchmarkProvider common.BenchmarkPhaseProvider,
	metadata common.CacheConfigMetadata,
	exporter EnvExporter,
) {
	phase, err := benchmarkProvider.GetBenchmarkPhase(common.BuildToolXcode, metadata)
	if err != nil {
		logger.Debugf("Failed to fetch benchmark phase, using configured flags: %v", err)

		return
	}

	if phase == "" {
		logger.Debugf("No benchmark phase found, using configured flags")

		return
	}

	envVar := common.BenchmarkPhaseEnvVar(common.BuildToolXcode)
	exporter.Export(envVar, phase)
	exporter.ExportToShellRC("Bitrise Benchmark Phase", "export "+envVar+"="+phase)
	common.WriteBenchmarkPhaseFile(common.BuildToolXcode, phase, logger)

	// The user-facing summary is logged once at the end of activation by
	// common.LogBenchmarkSummary. Avoid logging per-tool here so that on
	// multi-tool activations (React Native) the user does not see one
	// tool's baseline mode warning and assume the whole build is in
	// baseline when the relevant tool is actually caching normally.
	if phase == common.BenchmarkPhaseBaseline {
		params.BuildCacheEnabled = false
	}
}
