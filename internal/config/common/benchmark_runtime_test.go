//go:build unit

package common_test

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

type countingProvider struct {
	phase string
	err   error
	calls int
}

func (p *countingProvider) GetBenchmarkPhase(string, common.CacheConfigMetadata) (string, error) {
	p.calls++

	return p.phase, p.err
}

func buildMeta(buildID string) common.CacheConfigMetadata {
	return common.CacheConfigMetadata{CIProvider: common.CIProviderBitrise, BitriseAppID: "app-1", BitriseBuildID: buildID}
}

func phaseHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	// The override short-circuits everything; make sure a stray one cannot make
	// these tests pass for the wrong reason.
	t.Setenv(common.BenchmarkPhaseEnvVar(common.BuildToolGradle), "")
}

// One build runs a tool many times. All of them must agree, and the API must be
// asked once — the whole reason the phase is recorded rather than re-queried.
func TestResolveBenchmarkPhase_AsksOncePerBuild(t *testing.T) {
	phaseHome(t)
	provider := &countingProvider{phase: common.BenchmarkPhaseWarmup}

	for range 3 {
		assert.Equal(t, common.BenchmarkPhaseWarmup,
			common.ResolveBenchmarkPhase(common.BuildToolGradle, buildMeta("build-1"), provider, log.NewLogger()))
	}

	assert.Equal(t, 1, provider.calls, "later invocations must read the record, not re-ask")
}

// A persistent runner reuses the machine. Without build scoping, one build's
// baseline would disable the cache for every build that followed it.
func TestResolveBenchmarkPhase_ANewBuildAsksAgain(t *testing.T) {
	phaseHome(t)
	provider := &countingProvider{phase: common.BenchmarkPhaseBaseline}

	common.ResolveBenchmarkPhase(common.BuildToolGradle, buildMeta("build-1"), provider, log.NewLogger())
	common.ResolveBenchmarkPhase(common.BuildToolGradle, buildMeta("build-2"), provider, log.NewLogger())

	assert.Equal(t, 2, provider.calls, "a different build must not inherit the previous build's phase")
}

// A recorded phase is served to the build's later invocations without asking
// again. (This reaches the record, not the merge — the merge only runs when two
// invocations race, which TestRecordBenchmarkPhase_BaselineOutranksWarmup covers.)
func TestResolveBenchmarkPhase_ARecordedPhaseIsServedToLaterInvocations(t *testing.T) {
	phaseHome(t)
	meta := buildMeta("build-1")

	common.ResolveBenchmarkPhase(common.BuildToolGradle, meta, &countingProvider{phase: common.BenchmarkPhaseBaseline}, log.NewLogger())
	got := common.ResolveBenchmarkPhase(common.BuildToolGradle, meta, &countingProvider{phase: common.BenchmarkPhaseWarmup}, log.NewLogger())

	assert.Equal(t, common.BenchmarkPhaseBaseline, got)
}

// An unreachable benchmark API must not decide how a build caches, and must not
// leave a phase behind for the readers that ignore the build scope.
func TestResolveBenchmarkPhase_AnAPIFailureIsNotAPhase(t *testing.T) {
	phaseHome(t)
	provider := &countingProvider{err: errors.New("benchmark API unreachable")}

	got := common.ResolveBenchmarkPhase(common.BuildToolGradle, buildMeta("build-1"), provider, log.NewLogger())

	assert.Empty(t, got)
	assert.Empty(t, common.ReadBenchmarkPhaseFile(common.BuildToolGradle, log.NewLogger()))
}

// The client retries three times against a 10s timeout, and a build runs the
// tool dozens of times. One failure per build is the most it may cost.
func TestResolveBenchmarkPhase_AFailureIsNotRetriedByTheSameBuild(t *testing.T) {
	phaseHome(t)
	provider := &countingProvider{err: errors.New("benchmark API unreachable")}

	for range 3 {
		assert.Empty(t, common.ResolveBenchmarkPhase(common.BuildToolGradle, buildMeta("build-1"), provider, log.NewLogger()))
	}

	assert.Equal(t, 1, provider.calls)
}

// Every other reader of the file — LogBenchmarkSummary, the React Native
// activator, the Gradle plugin — ignores buildId. A record the resolver decided
// not to use must therefore not survive it.
func TestResolveBenchmarkPhase_APreviousBuildsRecordIsNotLeftOnDisk(t *testing.T) {
	phaseHome(t)

	common.RecordBenchmarkPhase(common.BuildToolGradle, buildMeta("build-1"), common.BenchmarkPhaseBaseline, log.NewLogger())

	// No provider: this build gets no phase of its own, which is exactly when a
	// stale record would be read as its answer.
	got := common.ResolveBenchmarkPhase(common.BuildToolGradle, buildMeta("build-2"), nil, log.NewLogger())

	assert.Empty(t, got)
	assert.Empty(t, common.ReadBenchmarkPhaseFile(common.BuildToolGradle, log.NewLogger()),
		"build-1's baseline must not be readable by build-2")
}

// An unmapped CI provider (Jenkins, Bamboo) and a local run both have no build
// ID. Merging by an empty ID matches every other unscoped record, so a one-off
// baseline would outrank every later warmup and pin the machine.
func TestRecordBenchmarkPhase_AnUnscopedBaselineDoesNotPinTheMachine(t *testing.T) {
	phaseHome(t)
	unscoped := common.CacheConfigMetadata{}
	require.Empty(t, common.BenchmarkBuildID(unscoped))

	common.RecordBenchmarkPhase(common.BuildToolGradle, unscoped, common.BenchmarkPhaseBaseline, log.NewLogger())
	common.RecordBenchmarkPhase(common.BuildToolGradle, unscoped, common.BenchmarkPhaseWarmup, log.NewLogger())

	assert.Equal(t, common.BenchmarkPhaseWarmup, common.ReadBenchmarkPhaseFile(common.BuildToolGradle, log.NewLogger()))
}

// A Bitrise build ID is the scope when there is one; off Bitrise — GitHub
// Actions, Build Hub, GitLab, CircleCI — the external one is, and it is the
// only thing keeping a persistent runner's builds apart.
func TestBenchmarkBuildID_FallsBackToTheExternalBuildID(t *testing.T) {
	assert.Equal(t, "bitrise-1",
		common.BenchmarkBuildID(common.CacheConfigMetadata{BitriseBuildID: "bitrise-1", ExternalBuildID: "ext-1"}))
	assert.Equal(t, "ext-1", common.BenchmarkBuildID(common.CacheConfigMetadata{ExternalBuildID: "ext-1"}))
}

// The same machine, two GitHub Actions builds: without the external fallback
// both records are unscoped, and the second build would inherit the first's
// phase instead of asking for its own.
func TestResolveBenchmarkPhase_AnExternalCIBuildDoesNotInheritThePreviousOne(t *testing.T) {
	phaseHome(t)
	external := func(buildID string) common.CacheConfigMetadata {
		return common.CacheConfigMetadata{CIProvider: "github-actions", ExternalAppID: "app-1", ExternalBuildID: buildID}
	}
	provider := &countingProvider{phase: common.BenchmarkPhaseBaseline}

	common.ResolveBenchmarkPhase(common.BuildToolGradle, external("ext-1"), provider, log.NewLogger())
	common.ResolveBenchmarkPhase(common.BuildToolGradle, external("ext-2"), provider, log.NewLogger())

	assert.Equal(t, 2, provider.calls, "each external build must ask for its own phase")
}

// e2e workflows pin a phase through the env var. Recording it would make the pin
// outlive the build that set it.
func TestResolveBenchmarkPhase_TheOverrideWinsAndIsNotRecorded(t *testing.T) {
	phaseHome(t)
	t.Setenv(common.BenchmarkPhaseEnvVar(common.BuildToolGradle), "established")
	provider := &countingProvider{phase: common.BenchmarkPhaseBaseline}

	got := common.ResolveBenchmarkPhase(common.BuildToolGradle, buildMeta("build-1"), provider, log.NewLogger())

	assert.Equal(t, "established", got)
	assert.Zero(t, provider.calls)
	path, err := common.BenchmarkPhaseFilePath(common.BuildToolGradle)
	require.NoError(t, err)
	assert.NoFileExists(t, path)
}

// "no phase" is an answer worth recording, or every invocation of an ordinary
// build pays for a request that returns nothing.
func TestResolveBenchmarkPhase_NoPhaseIsStillRecorded(t *testing.T) {
	phaseHome(t)
	provider := &countingProvider{phase: ""}

	assert.Empty(t, common.ResolveBenchmarkPhase(common.BuildToolGradle, buildMeta("build-1"), provider, log.NewLogger()))
	assert.Empty(t, common.ResolveBenchmarkPhase(common.BuildToolGradle, buildMeta("build-1"), provider, log.NewLogger()))

	assert.Equal(t, 1, provider.calls)
}

// The Gradle plugin reads this file by its legacy shape; the build scoping is
// additive and must not change what that reader sees.
func TestResolveBenchmarkPhase_RecordKeepsTheLegacyPhaseKey(t *testing.T) {
	phaseHome(t)

	common.ResolveBenchmarkPhase(common.BuildToolGradle, buildMeta("build-1"),
		&countingProvider{phase: common.BenchmarkPhaseWarmup}, log.NewLogger())

	path, err := common.BenchmarkPhaseFilePath(common.BuildToolGradle)
	require.NoError(t, err)
	body, err := os.ReadFile(path)
	require.NoError(t, err)

	var legacy struct {
		Phase string `json:"phase"`
	}
	require.NoError(t, json.Unmarshal(body, &legacy))
	assert.Equal(t, common.BenchmarkPhaseWarmup, legacy.Phase)
}

// The activation path writes through this wrapper, and it was briefly gutted to
// a no-op without a single test noticing — only the linter did. A build-scoped
// record written at activation is what lets the build's own invocations skip
// the query, so its absence is silent and costs a request every time.
func TestRecordBenchmarkPhase_WritesARecordTheResolverThenReuses(t *testing.T) {
	phaseHome(t)
	meta := buildMeta("build-1")

	common.RecordBenchmarkPhase(common.BuildToolGradle, meta, common.BenchmarkPhaseWarmup, log.NewLogger())

	// No provider: if the record were not written, this could only return "".
	got := common.ResolveBenchmarkPhase(common.BuildToolGradle, meta, nil, log.NewLogger())

	assert.Equal(t, common.BenchmarkPhaseWarmup, got)
}
