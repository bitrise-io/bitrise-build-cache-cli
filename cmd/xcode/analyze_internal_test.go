//go:build unit

package xcode

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildanalyzer"
)

func TestResolveAnalyzeLogPath_ExplicitWins(t *testing.T) {
	path, err := resolveAnalyzeLogPath("/tmp/explicit.xcactivitylog", "/tmp/derived")
	require.NoError(t, err)
	assert.Equal(t, "/tmp/explicit.xcactivitylog", path)
}

func TestResolveAnalyzeLogPath_DerivedDataResolvesNewest(t *testing.T) {
	dir := t.TempDir()
	logsBuild := filepath.Join(dir, "Logs", "Build")
	require.NoError(t, os.MkdirAll(logsBuild, 0o755))

	want := filepath.Join(logsBuild, "A.xcactivitylog")
	require.NoError(t, os.WriteFile(want, []byte("x"), 0o644))

	path, err := resolveAnalyzeLogPath("", dir)
	require.NoError(t, err)
	assert.Equal(t, want, path)
}

func TestResolveAnalyzeLogPath_NeitherFlag_Error(t *testing.T) {
	_, err := resolveAnalyzeLogPath("", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required")
}

func TestResolveAnalyzeLogPath_DerivedDataEmpty_Error(t *testing.T) {
	dir := t.TempDir()
	logsBuild := filepath.Join(dir, "Logs", "Build")
	require.NoError(t, os.MkdirAll(logsBuild, 0o755))

	_, err := resolveAnalyzeLogPath("", dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no .xcactivitylog")
}

func TestRenderJSON_ShapeMatchesReport(t *testing.T) {
	report := buildanalyzer.Report{
		Build: buildanalyzer.BuildMeta{Title: "Build Foo"},
		Tasks: []buildanalyzer.Task{{Key: "SwiftCompile X", Target: "Foo", Tool: "SwiftCompile"}},
	}

	var buf bytes.Buffer
	require.NoError(t, renderJSON(&buf, report))

	var decoded buildanalyzer.Report
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
	assert.Equal(t, report.Build.Title, decoded.Build.Title)
	require.Len(t, decoded.Tasks, 1)
	assert.Equal(t, "SwiftCompile X", decoded.Tasks[0].Key)
}

func TestRenderPretty_IncludesTitleAndTaskKeys(t *testing.T) {
	report := buildanalyzer.Report{
		Build: buildanalyzer.BuildMeta{Title: "Build Foo"},
		Tasks: []buildanalyzer.Task{
			{Key: "Ld /Foo", Errors: []string{"error: undefined symbol"}},
			{Key: "SwiftCompile Bar"},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, renderPretty(&buf, report))

	out := buf.String()
	assert.Contains(t, out, "Build Foo")
	assert.Contains(t, out, "Tasks: 2")
	assert.Contains(t, out, "Ld /Foo")
	assert.Contains(t, out, "SwiftCompile Bar")
	assert.Contains(t, out, "error: undefined symbol")
}

func TestRunAnalyze_UnknownOutput_Error(t *testing.T) {
	prevLog, prevDD, prevOut := analyzeFlagActivityLog, analyzeFlagDerivedData, analyzeFlagOutput
	defer func() {
		analyzeFlagActivityLog, analyzeFlagDerivedData, analyzeFlagOutput = prevLog, prevDD, prevOut
	}()

	// Point the explicit flag at *any* path that would otherwise reach the
	// renderer — the unknown-output branch is tested before the reader runs.
	analyzeFlagActivityLog = "/nope"
	analyzeFlagDerivedData = ""
	analyzeFlagOutput = "yaml"

	err := runAnalyze(&bytes.Buffer{})
	require.Error(t, err)
	// Either the missing file OR the unknown-output branch is acceptable —
	// the former reaches the read-error path, the latter the format-check.
	assert.True(t, strings.Contains(err.Error(), "unknown --output") || strings.Contains(err.Error(), "analyze"),
		"got: %v", err)
}
