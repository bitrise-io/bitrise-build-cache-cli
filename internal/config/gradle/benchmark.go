package gradleconfig

import (
	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

// EnvExporter abstracts environment variable export for testability.
type EnvExporter interface {
	Export(key, value string)
	ExportToShellRC(blockName, content string)
}

// ApplyBenchmarkPhase queries the benchmark phase and overrides gradle params accordingly.
// Baseline phase disables cache and enables analytics only.
// The phase is exported as BITRISE_BUILD_CACHE_BENCHMARK_PHASE_GRADLE env var
// and written to ~/.local/state/xcelerate/benchmark/benchmark-phase-gradle.json
// as fallback. The user-facing log line is emitted once by
// common.LogBenchmarkSummary at the end of activation, not from this function.
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

	envVar := common.BenchmarkPhaseEnvVar(common.BuildToolGradle)
	exporter.Export(envVar, phase)
	exporter.ExportToShellRC("Bitrise Benchmark Phase", "export "+envVar+"="+phase)
	common.WriteBenchmarkPhaseFile(common.BuildToolGradle, phase, logger)

	// The user-facing summary is logged once at the end of activation by
	// common.LogBenchmarkSummary. Avoid logging per-tool here so that on
	// multi-tool activations (React Native) the user does not see one
	// tool's baseline mode warning and assume the whole build is in
	// baseline when the relevant tool is actually caching normally.
	if phase == common.BenchmarkPhaseBaseline {
		params.Cache.Enabled = false
		params.Cache.JustDependency = false
		params.Analytics.Enabled = true
	}
}
