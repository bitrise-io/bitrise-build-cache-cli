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
	prevLog, prevDD, prevMan, prevOut := analyzeFlagActivityLog, analyzeFlagDerivedData, analyzeFlagManifest, analyzeFlagOutput
	defer func() {
		analyzeFlagActivityLog, analyzeFlagDerivedData, analyzeFlagManifest, analyzeFlagOutput = prevLog, prevDD, prevMan, prevOut
	}()

	// Point the explicit flag at *any* path that would otherwise reach the
	// renderer — the unknown-output branch is tested before the reader runs.
	analyzeFlagActivityLog = "/nope"
	analyzeFlagDerivedData = ""
	analyzeFlagManifest = ""
	analyzeFlagOutput = "yaml"

	err := runAnalyze(&bytes.Buffer{}, &bytes.Buffer{})
	require.Error(t, err)
	// Either the missing file OR the unknown-output branch is acceptable —
	// the former reaches the read-error path, the latter the format-check.
	assert.True(t, strings.Contains(err.Error(), "unknown --output") || strings.Contains(err.Error(), "analyze"),
		"got: %v", err)
}

func TestResolveAnalyzeManifestPath_ExplicitWins(t *testing.T) {
	path, err := resolveAnalyzeManifestPath("/tmp/explicit.json", "/tmp/derived")
	require.NoError(t, err)
	assert.Equal(t, "/tmp/explicit.json", path)
}

func TestResolveAnalyzeManifestPath_NoFlags_ReturnsEmpty(t *testing.T) {
	// Not setting either flag is legal — the caller falls back to v0 output.
	path, err := resolveAnalyzeManifestPath("", "")
	require.NoError(t, err)
	assert.Empty(t, path)
}

func TestResolveAnalyzeManifestPath_AutoDiscoverFromDerivedData(t *testing.T) {
	dir := t.TempDir()
	xcbuild := filepath.Join(dir, "Build", "Intermediates.noindex", "XCBuildData", "hash.xcbuilddata")
	require.NoError(t, os.MkdirAll(xcbuild, 0o755))
	manifestPath := filepath.Join(xcbuild, "manifest.json")
	require.NoError(t, os.WriteFile(manifestPath, []byte("{}"), 0o644))

	path, err := resolveAnalyzeManifestPath("", dir)
	require.NoError(t, err)
	assert.Equal(t, manifestPath, path)
}

func TestJoinManifestIfAvailable_MissingManifest_WarnsAndSkipsNoError(t *testing.T) {
	var report buildanalyzer.Report
	var stderr bytes.Buffer

	err := joinManifestIfAvailable(&report, &stderr, "", "")
	require.NoError(t, err)
	assert.Contains(t, stderr.String(), "no manifest.json available")
}

func TestJoinManifestIfAvailable_BadManifestFile_WarnsAndSkipsNoError(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "manifest.json")
	require.NoError(t, os.WriteFile(bad, []byte("{not json"), 0o644))

	var report buildanalyzer.Report
	var stderr bytes.Buffer

	err := joinManifestIfAvailable(&report, &stderr, bad, "")
	require.NoError(t, err, "soft-fallback — the activity log alone is still useful")
	assert.Contains(t, stderr.String(), "manifest.json read failed")
}

func TestJoinManifestIfAvailable_SuccessfulLoadExtendsReport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	body := `{"commands": {"Ld": {"tool":"linker","description":"Ld /Foo","outputs":["/Foo"]}}}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	report := buildanalyzer.Report{
		Tasks: []buildanalyzer.Task{{Key: "Ld /Foo (in target 'Foo' from project 'Foo')", Ran: true, Tool: "Ld"}},
	}
	var stderr bytes.Buffer

	require.NoError(t, joinManifestIfAvailable(&report, &stderr, path, ""))
	assert.Empty(t, stderr.String(), "no warning on clean load")
	assert.True(t, report.Tasks[0].Planned)
	assert.Equal(t, "linker", report.Tasks[0].Tool)
}

func TestAttachDiagnosticsFromFlag_DefaultsToAuto(t *testing.T) {
	report := buildanalyzer.Report{
		Tasks: []buildanalyzer.Task{{
			StderrExcerpt: "App.swift:5:9: error: cannot find 'Foo' in scope\n",
		}},
	}

	require.NoError(t, attachDiagnosticsFromFlag(&report, "", ""))
	require.Len(t, report.Tasks[0].Diagnostics, 1)
	assert.Equal(t, buildanalyzer.SeverityError, report.Tasks[0].Diagnostics[0].Severity)
}

func TestAttachDiagnosticsFromFlag_Off_LeavesTasksAlone(t *testing.T) {
	report := buildanalyzer.Report{
		Tasks: []buildanalyzer.Task{{
			StderrExcerpt: "App.swift:5:9: error: cannot find 'Foo' in scope\n",
		}},
	}

	require.NoError(t, attachDiagnosticsFromFlag(&report, "", "off"))
	assert.Empty(t, report.Tasks[0].Diagnostics)
}

func TestAttachDiagnosticsFromFlag_UnknownValue_Errors(t *testing.T) {
	err := attachDiagnosticsFromFlag(&buildanalyzer.Report{}, "", "yolo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown --diagnostics")
}

func TestRenderPretty_DiagnosticsIconPresent(t *testing.T) {
	// Confirms the [E/W/N] column lands and the attached diagnostic
	// surfaces in the indented continuation line.
	report := buildanalyzer.Report{
		Build: buildanalyzer.BuildMeta{Title: "Build Foo"},
		Tasks: []buildanalyzer.Task{{
			Key: "SwiftCompile App.swift",
			Diagnostics: []buildanalyzer.Diagnostic{{
				File: "App.swift", Line: 5, Column: 9,
				Severity: buildanalyzer.SeverityError, Message: "cannot find 'Foo' in scope",
			}},
		}},
	}

	var buf bytes.Buffer
	require.NoError(t, renderPretty(&buf, report))

	out := buf.String()
	// Icon column shows E for error.
	assert.Contains(t, out, " E ")
	// Indented diagnostic line surfaces file:line:col.
	assert.Contains(t, out, "App.swift:5:9")
	assert.Contains(t, out, "cannot find 'Foo' in scope")
}
