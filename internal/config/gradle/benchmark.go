package gradleconfig

import (
	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
)

// EnvExporter abstracts environment variable export for testability.
type EnvExporter interface {
	Export(key, value string)
	ExportToShellRC(blockName, content string)
}

// ApplyBenchmarkPhase queries the benchmark phase and overrides gradle params accordingly.
// Baseline phase disables cache and enables analytics only. Warmup phase logs a warning.
// The phase is exported as the per-build-tool BITRISE_BUILD_CACHE_BENCHMARK_PHASE_GRADLE
// env var and written to ~/.local/state/xcelerate/benchmark/benchmark-phase-gradle.json.
// For back-compat with older gradle-analytics plugins (< 2.6.1) that still read the
// pre-split BITRISE_BUILD_CACHE_BENCHMARK_PHASE env var and benchmark-phase.json file,
// the gradle phase is mirrored to those legacy names too.
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
	logger.Infof("(i) Benchmark phase: %s", phase)
	exporter.Export(envVar, phase)
	exporter.ExportToShellRC("Bitrise Benchmark Phase", "export "+envVar+"="+phase)
	common.WriteBenchmarkPhaseFile(common.BuildToolGradle, phase, logger)

	// Back-compat: pre-2.6.1 gradle-analytics plugins read the legacy env var
	// and file name. Mirror the gradle phase so older plugin versions still
	// tag their invocations correctly.
	exporter.Export(common.LegacyBenchmarkPhaseEnvVar, phase)
	common.WriteLegacyBenchmarkPhaseFile(phase, logger)

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
