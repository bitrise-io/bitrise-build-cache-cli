//go:build unit

package collect

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTimingCollector_parsesKnownTaskTypes(t *testing.T) {
	tail := []byte(`
Build Timing Summary

    CompileSwift (120 tasks) | 56.789 seconds
    CompileC (18 tasks) | 3.21 seconds
    Ld (4 tasks) | 1.0 seconds
    CodeSign (3 tasks) | 0.75 seconds
    PhaseScriptExecution (6 tasks) | 2.4 seconds
    CompileAssetCatalog (1 task) | 0.5 seconds
    SomeUnknownTaskType (99 tasks) | 10 seconds
`)

	out, err := BuildTimingCollector{}.Collect(context.Background(), Input{XcodebuildInfoBytes: tail})
	require.NoError(t, err)

	require.NotNil(t, out.BuildTimings)
	buckets := out.BuildTimings.Buckets

	assert.Equal(t, 120, buckets["CompileSwift"].Count)
	assert.InDelta(t, 56.789, buckets["CompileSwift"].TotalSeconds, 1e-9)
	assert.Equal(t, 18, buckets["CompileC"].Count)
	assert.InDelta(t, 3.21, buckets["CompileC"].TotalSeconds, 1e-9)
	assert.Equal(t, 4, buckets["Ld"].Count)
	assert.Equal(t, 3, buckets["CodeSign"].Count)
	assert.Equal(t, 6, buckets["PhaseScriptExecution"].Count)
	assert.Equal(t, 1, buckets["CompileAssetCatalog"].Count)

	_, hasUnknown := buckets["SomeUnknownTaskType"]
	assert.False(t, hasUnknown, "non-allow-listed task types must not reach the payload")
}

func TestBuildTimingCollector_emptyInput_noOp(t *testing.T) {
	out, err := BuildTimingCollector{}.Collect(context.Background(), Input{})
	require.NoError(t, err)
	assert.Nil(t, out.BuildTimings)
}

func TestBuildTimingCollector_noMatchingLines_noOp(t *testing.T) {
	out, err := BuildTimingCollector{}.Collect(context.Background(), Input{XcodebuildInfoBytes: []byte("no match here\n")})
	require.NoError(t, err)
	assert.Nil(t, out.BuildTimings)
}
