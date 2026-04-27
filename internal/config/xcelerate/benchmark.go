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
// Baseline phase disables cache. Warmup phase logs a warning.
// The phase is exported as the per-build-tool BITRISE_BUILD_CACHE_BENCHMARK_PHASE_XCODE
// env var and written to ~/.local/state/xcelerate/benchmark/benchmark-phase-xcode.json.
// The legacy BITRISE_BUILD_CACHE_BENCHMARK_PHASE env var is intentionally NOT mirrored
// here — only the gradle activation path writes it, to avoid clobbering the gradle
// phase in mixed (e.g. React Native) activations where the legacy reader is the
// gradle plugin.
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
	logger.Infof("(i) Benchmark phase: %s", phase)
	exporter.Export(envVar, phase)
	exporter.ExportToShellRC("Bitrise Benchmark Phase", "export "+envVar+"="+phase)
	common.WriteBenchmarkPhaseFile(common.BuildToolXcode, phase, logger)

	switch phase {
	case common.BenchmarkPhaseBaseline:
		logger.Warnf("Benchmark baseline mode: disabling cache and enabling analytics only")
		params.BuildCacheEnabled = false
	case common.BenchmarkPhaseWarmup:
		logger.Infof("(i) Benchmark warmup phase: cache performance might not be ideal")
	}
}
