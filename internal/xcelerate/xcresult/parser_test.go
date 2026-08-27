//go:build unit

package xcresult

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_parseBuildResults_extractsTargetsAndFailures(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "build-results.json"))
	require.NoError(t, err)

	summary, err := parseBuildResults(raw)
	require.NoError(t, err)

	// Targets are de-duped by name across `actions[].targets` and top-level `targets`.
	require.Len(t, summary.Targets, 3)

	byName := map[string]TargetSummary{}
	for _, t := range summary.Targets {
		byName[t.Name] = t
	}

	assert.Equal(t, int64(12500), byName["Seek"].BuildDurationMs)
	assert.Equal(t, int64(2500), byName["SeekTests"].BuildDurationMs, "buildDurationInSeconds must convert to ms")
	assert.Equal(t, int64(3200), byName["SharedKit"].BuildDurationMs)

	require.Len(t, summary.Failures, 1)
	assert.Equal(t, "Seek", summary.Failures[0].TargetName)
	assert.Equal(t, "SwiftCompile normal arm64 Compiling Foo.swift", summary.Failures[0].Message,
		"failure message must be trimmed to the first line")
}

func Test_parseBuildResults_emptyInput(t *testing.T) {
	summary, err := parseBuildResults(nil)
	require.NoError(t, err)
	assert.Empty(t, summary.Targets)
	assert.Empty(t, summary.Failures)
}

func Test_parseBuildResults_whitespaceOnly(t *testing.T) {
	summary, err := parseBuildResults([]byte("   \n\t\r "))
	require.NoError(t, err)
	assert.Empty(t, summary.Targets)
}

func Test_parseBuildResults_toleratesUnknownFields(t *testing.T) {
	raw := []byte(`{
		"unknown_top_level": {"nested": [1,2,3]},
		"targets": [{"name": "OnlyOne", "buildDurationInMs": 42}]
	}`)

	summary, err := parseBuildResults(raw)
	require.NoError(t, err)
	require.Len(t, summary.Targets, 1)
	assert.Equal(t, "OnlyOne", summary.Targets[0].Name)
	assert.Equal(t, int64(42), summary.Targets[0].BuildDurationMs)
}

func Test_parseBuildResults_malformedJSON(t *testing.T) {
	_, err := parseBuildResults([]byte(`{not json`))
	require.Error(t, err)
}

func Test_parseBuildResults_producingTargetFallback(t *testing.T) {
	raw := []byte(`{
		"errors": [
			{"message": "linker error\n  extra context", "producingTarget": {"targetName": "App"}}
		]
	}`)

	summary, err := parseBuildResults(raw)
	require.NoError(t, err)
	require.Len(t, summary.Failures, 1)
	assert.Equal(t, "App", summary.Failures[0].TargetName)
	assert.Equal(t, "linker error", summary.Failures[0].Message)
}

func Test_parseBuildResults_durationFromStartEnd(t *testing.T) {
	raw := []byte(`{
		"targets": [
			{"name": "T1", "startedTime": 100.0, "endedTime": 103.5}
		]
	}`)

	summary, err := parseBuildResults(raw)
	require.NoError(t, err)
	require.Len(t, summary.Targets, 1)
	assert.Equal(t, int64(3500), summary.Targets[0].BuildDurationMs)
}

func Test_parseBuildResults_metricsFallback(t *testing.T) {
	raw := []byte(`{
		"targets": [
			{"name": "T1", "metrics": {"buildDurationInMs": 999}}
		]
	}`)

	summary, err := parseBuildResults(raw)
	require.NoError(t, err)
	require.Len(t, summary.Targets, 1)
	assert.Equal(t, int64(999), summary.Targets[0].BuildDurationMs)
}

func Test_firstLine(t *testing.T) {
	assert.Equal(t, "hello", firstLine("hello"))
	assert.Equal(t, "hello", firstLine("hello\nworld"))
	assert.Equal(t, "hello", firstLine("   hello   \n"))
	assert.Empty(t, firstLine(""))
	assert.Empty(t, firstLine("   \n   "))
}
