//go:build unit

package common

import (
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
)

// The merge only runs when two invocations of one build query concurrently and
// both write. Going through ResolveBenchmarkPhase cannot reach it — the second
// caller reads the first one's record and returns before it ever queries — so
// the precedence has to be exercised here, directly.
func TestRecordBenchmarkPhase_BaselineOutranksWarmup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// The racing pair, in the order that would lose the phase if rank were ignored.
	recordBenchmarkPhase(BuildToolGradle, "build-1", BenchmarkPhaseBaseline, log.NewLogger())
	got := recordBenchmarkPhase(BuildToolGradle, "build-1", BenchmarkPhaseWarmup, log.NewLogger())

	assert.Equal(t, BenchmarkPhaseBaseline, got,
		"half a build caching and half not would make the measurement meaningless")
}

// The ranking must not pin a phase across builds — only within one.
func TestRecordBenchmarkPhase_DoesNotCarryRankAcrossBuilds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	recordBenchmarkPhase(BuildToolGradle, "build-1", BenchmarkPhaseBaseline, log.NewLogger())
	got := recordBenchmarkPhase(BuildToolGradle, "build-2", BenchmarkPhaseWarmup, log.NewLogger())

	assert.Equal(t, BenchmarkPhaseWarmup, got)
}
