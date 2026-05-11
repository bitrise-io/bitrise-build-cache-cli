//go:build unit

package common

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingLogger captures the Info / Warn lines emitted during a test.
// Embeds log.Logger so the value satisfies the full interface; only Infof
// and Warnf are overridden because those are the ones LogBenchmarkSummary
// uses.
type recordingLogger struct {
	log.Logger
	infoLines []string
	warnLines []string
}

func (r *recordingLogger) Infof(format string, args ...any) {
	r.infoLines = append(r.infoLines, fmt.Sprintf(format, args...))
}

func (r *recordingLogger) Warnf(format string, args ...any) {
	r.warnLines = append(r.warnLines, fmt.Sprintf(format, args...))
}

func TestLogBenchmarkSummary_NoPhasesNoOp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	rl := &recordingLogger{Logger: log.NewLogger()}
	LogBenchmarkSummary(rl, []string{BuildToolGradle, BuildToolXcode})

	assert.Empty(t, rl.infoLines, "should not emit info when no phase files exist")
	assert.Empty(t, rl.warnLines, "should not emit warn when no phase files exist")
}

func TestLogBenchmarkSummary_AnyToolNotBaselineUsesInfo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	WriteBenchmarkPhaseFile(BuildToolGradle, BenchmarkPhaseWarmup, log.NewLogger())
	WriteBenchmarkPhaseFile(BuildToolXcode, BenchmarkPhaseBaseline, log.NewLogger())

	rl := &recordingLogger{Logger: log.NewLogger()}
	LogBenchmarkSummary(rl, []string{BuildToolGradle, BuildToolXcode})

	require.Len(t, rl.infoLines, 1)
	assert.Empty(t, rl.warnLines)

	line := rl.infoLines[0]
	assert.Contains(t, line, "gradle=warmup (cache enabled")
	assert.Contains(t, line, "xcode=baseline (cache disabled)")
}

func TestLogBenchmarkSummary_AllBaselineUsesWarn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	WriteBenchmarkPhaseFile(BuildToolGradle, BenchmarkPhaseBaseline, log.NewLogger())
	WriteBenchmarkPhaseFile(BuildToolXcode, BenchmarkPhaseBaseline, log.NewLogger())

	rl := &recordingLogger{Logger: log.NewLogger()}
	LogBenchmarkSummary(rl, []string{BuildToolGradle, BuildToolXcode})

	assert.Empty(t, rl.infoLines)
	require.Len(t, rl.warnLines, 1)

	line := rl.warnLines[0]
	assert.Contains(t, line, "All active build tools in baseline benchmark mode")
	assert.Contains(t, line, "gradle=baseline")
	assert.Contains(t, line, "xcode=baseline")
}

func TestLogBenchmarkSummary_SkipsToolsWithoutPhase(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	WriteBenchmarkPhaseFile(BuildToolGradle, BenchmarkPhaseWarmup, log.NewLogger())

	rl := &recordingLogger{Logger: log.NewLogger()}
	LogBenchmarkSummary(rl, []string{BuildToolGradle, BuildToolXcode})

	require.Len(t, rl.infoLines, 1)

	line := rl.infoLines[0]
	assert.Contains(t, line, "gradle=warmup")
	assert.NotContains(t, line, "xcode")
}

func TestLogBenchmarkSummary_SingleToolBaselineWarns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	WriteBenchmarkPhaseFile(BuildToolGradle, BenchmarkPhaseBaseline, log.NewLogger())

	rl := &recordingLogger{Logger: log.NewLogger()}
	LogBenchmarkSummary(rl, []string{BuildToolGradle})

	assert.Empty(t, rl.infoLines)
	require.Len(t, rl.warnLines, 1)
	assert.Contains(t, rl.warnLines[0], "gradle=baseline")
}

func TestLogBenchmarkSummary_OutputIsToolOrderStable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	WriteBenchmarkPhaseFile(BuildToolXcode, BenchmarkPhaseWarmup, log.NewLogger())
	WriteBenchmarkPhaseFile(BuildToolGradle, BenchmarkPhaseWarmup, log.NewLogger())

	rl := &recordingLogger{Logger: log.NewLogger()}
	LogBenchmarkSummary(rl, []string{BuildToolXcode, BuildToolGradle})

	require.Len(t, rl.infoLines, 1)
	line := rl.infoLines[0]

	// Tools sorted alphabetically: gradle before xcode.
	assert.Less(t, strings.Index(line, "gradle="), strings.Index(line, "xcode="))
}
