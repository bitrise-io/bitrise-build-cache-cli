package common

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"
)

// Lite activation cannot ask for a benchmark phase: the query is keyed on the
// workspace, app and workflow, none of which exist at VM warmup. The build asks
// instead — but a build runs the tool many times, and the answer must be the
// same for all of them and cost one request.
//
// So the first invocation to get an answer records it, and the rest read the
// record. The record is scoped to a build: a persistent runner would otherwise
// serve one build's phase to every build that follows it, which is how a
// workspace gets pinned to baseline forever.

// phaseRank orders the phases so concurrent invocations converge on the same
// answer. baseline outranks warmup because it is the one that disables the
// cache: having half a build's invocations cache and half not would make the
// measurement meaningless, and the safe direction is the one being measured.
func phaseRank(phase string) int {
	switch phase {
	case BenchmarkPhaseBaseline:
		return 2
	case BenchmarkPhaseWarmup:
		return 1
	default:
		return 0
	}
}

// BenchmarkPhaseRecord is what the phase file holds. Phase alone is the legacy
// shape and is still what other readers (the Gradle plugin) look for, so the
// added fields are additive.
type BenchmarkPhaseRecord struct {
	Phase string `json:"phase"`
	// BuildID scopes the record. An empty one is a pre-scoping record, or one
	// written off CI, and is never reused.
	BuildID string `json:"buildId,omitempty"`
}

// BenchmarkBuildID identifies the build a phase record belongs to.
func BenchmarkBuildID(metadata CacheConfigMetadata) string {
	if metadata.BitriseBuildID != "" {
		return metadata.BitriseBuildID
	}

	return metadata.ExternalBuildID
}

// BenchmarkPhaseOverride returns the phase pinned through
// BITRISE_BUILD_CACHE_BENCHMARK_PHASE_<TOOL>, or "" when none is set.
func BenchmarkPhaseOverride(buildTool string) string {
	return os.Getenv(BenchmarkPhaseEnvVar(buildTool))
}

// ResolveBenchmarkPhase returns the phase for this build, asking the API at most
// once per build per tool however many invocations run. Errors resolve to "" —
// an unreachable benchmark API must not decide how a build caches.
func ResolveBenchmarkPhase(
	buildTool string,
	metadata CacheConfigMetadata,
	provider BenchmarkPhaseProvider,
	logger log.Logger,
) string {
	buildID := BenchmarkBuildID(metadata)

	// Every other reader of the file — LogBenchmarkSummary, the React Native
	// activator, the Gradle plugin — ignores buildId, so another build's record
	// left in place would be read as this build's phase.
	discardForeignRecord(buildTool, buildID, logger)

	// The override wins outright and is never recorded: e2e workflows set it to
	// pin a phase, and writing it would make that pin outlive the build.
	if phase := BenchmarkPhaseOverride(buildTool); phase != "" {
		logger.Debugf("Benchmark phase from env var: %s", phase)

		return phase
	}

	if record, found := readBenchmarkPhaseRecord(buildTool, logger); found && record.BuildID != "" && record.BuildID == buildID {
		logger.Debugf("Benchmark phase already resolved for this build: %q", record.Phase)

		return record.Phase
	}

	if provider == nil {
		return ""
	}

	phase, err := provider.GetBenchmarkPhase(buildTool, metadata)
	if err != nil {
		logger.Debugf("Could not fetch the benchmark phase, continuing without one: %v", err)
		// Recorded as "no phase" for this build only: a build runs the tool dozens
		// of times, and an unreachable API costs retries against a 10s timeout on
		// every one of them.
		recordBenchmarkPhase(buildTool, buildID, "", logger)

		return ""
	}

	return recordBenchmarkPhase(buildTool, buildID, phase, logger)
}

// RecordBenchmarkPhase stores a phase the caller already fetched, scoped to the
// build, so later invocations of the same build reuse it rather than asking again.
func RecordBenchmarkPhase(buildTool string, metadata CacheConfigMetadata, phase string, logger log.Logger) {
	recordBenchmarkPhase(buildTool, BenchmarkBuildID(metadata), phase, logger)
}

// recordBenchmarkPhase merges phase into the build's record and returns what the
// build should use. Re-reading first converges concurrent invocations on the
// higher-ranked phase in the common case; it is not a lock, so two that read
// before either writes can still race, and the later rename wins. Acceptable
// here: the window is two file operations wide, and the cost of losing it is a
// warmup-phase build measured as a baseline one, not a broken build.
func recordBenchmarkPhase(buildTool, buildID, phase string, logger log.Logger) string {
	if existing, found := readBenchmarkPhaseRecord(buildTool, logger); found &&
		buildID != "" && existing.BuildID == buildID && phaseRank(existing.Phase) > phaseRank(phase) {
		phase = existing.Phase
	}

	writeBenchmarkPhaseRecord(buildTool, BenchmarkPhaseRecord{Phase: phase, BuildID: buildID}, logger)

	return phase
}

// discardForeignRecord removes a record written for a different build. The
// readers that ignore buildId would otherwise read it as their own, and a
// resolve that ends without an answer (no provider, a failed query) would leave
// it there.
func discardForeignRecord(buildTool, buildID string, logger log.Logger) {
	record, found := readBenchmarkPhaseRecord(buildTool, logger)
	if !found || record.BuildID == buildID {
		return
	}

	path, err := BenchmarkPhaseFilePath(buildTool)
	if err != nil {
		logger.Debugf("Failed to get benchmark phase file path: %v", err)

		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		logger.Debugf("Failed to discard the previous build's benchmark phase record: %v", err)

		return
	}

	logger.Debugf("Discarded a benchmark phase record from build %q", record.BuildID)
}

func readBenchmarkPhaseRecord(buildTool string, logger log.Logger) (BenchmarkPhaseRecord, bool) {
	path, err := BenchmarkPhaseFilePath(buildTool)
	if err != nil {
		logger.Debugf("Failed to get benchmark phase file path: %v", err)

		return BenchmarkPhaseRecord{}, false
	}

	body, err := os.ReadFile(path) //nolint:gosec // path derived from home + constant
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			logger.Debugf("Failed to read benchmark phase file: %v", err)
		}

		return BenchmarkPhaseRecord{}, false
	}

	var record BenchmarkPhaseRecord
	if err := json.Unmarshal(body, &record); err != nil {
		logger.Debugf("Failed to decode benchmark phase file: %v", err)

		return BenchmarkPhaseRecord{}, false
	}

	return record, true
}

// writeBenchmarkPhaseRecord renames a temp file over the target so a reader
// never sees a half-written record, and a crash leaves the previous one intact.
func writeBenchmarkPhaseRecord(buildTool string, record BenchmarkPhaseRecord, logger log.Logger) {
	path, err := BenchmarkPhaseFilePath(buildTool)
	if err != nil {
		logger.Debugf("Failed to get benchmark phase file path: %v", err)

		return
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:mnd
		logger.Debugf("Failed to create benchmark phase dir: %v", err)

		return
	}

	body, err := json.Marshal(record)
	if err != nil {
		logger.Debugf("Failed to marshal benchmark phase record: %v", err)

		return
	}

	tmp, err := os.CreateTemp(dir, ".benchmark-phase-*.json")
	if err != nil {
		logger.Debugf("Failed to create temp benchmark phase file: %v", err)

		return
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		logger.Debugf("Failed to write temp benchmark phase file: %v", err)

		return
	}
	if err := tmp.Close(); err != nil {
		logger.Debugf("Failed to close temp benchmark phase file: %v", err)

		return
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		logger.Debugf("Failed to move the benchmark phase file into place: %v", err)

		return
	}

	logger.Debugf("Benchmark phase %q recorded for build %q at %s", record.Phase, record.BuildID, path)
}
