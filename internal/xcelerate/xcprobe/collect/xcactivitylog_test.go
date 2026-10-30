//go:build unit

package collect_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/collect"
)

// Fixture path shared across tests. The sample is Xcode 26.x (SLF v13) with
// 65 Swift cache hits and 320 attachments covering BuildOperationMetrics +
// TaskMetrics.
const slfFixture = "../slf/testdata/sample.xcactivitylog"

func TestActivityLogCollector_Phase1Surfaces(t *testing.T) {
	path, err := filepath.Abs(slfFixture)
	require.NoError(t, err)

	out, err := collect.ActivityLogCollector{}.Collect(context.Background(), collect.Input{
		ActivityLogPath: path,
		TargetTopN:      25,
		StepTopN:        25,
	})
	require.NoError(t, err)

	require.NotEmpty(t, out.Targets, "fixture carries at least one target")
	target := out.Targets[0]
	assert.Equal(t, "CasProbe", target.Name, "cleanTargetName trims the Xcode wrapper prefix")
	assert.Greater(t, target.StepCount, 0, "target step count must be populated")

	// StepTypeHist contains swiftCompilation at a minimum (the fixture only
	// has one Swift module but it still goes through the compiler step).
	seen := map[string]bool{}
	for _, b := range out.StepTypeHist {
		seen[b.Type] = true
	}

	assert.True(t, seen["swiftCompilation"] || seen["swiftAggregatedCompilation"], "swift compilation bucket must appear in StepTypeHist")

	assert.NotEmpty(t, out.TopSteps, "fixture must produce TopSteps")
	assert.Nil(t, out.AttachmentsCCM, "Phase 2 attachments gated by DecodeAttachments; off here")
}

func TestActivityLogCollector_Phase2Attachments(t *testing.T) {
	path, err := filepath.Abs(slfFixture)
	require.NoError(t, err)

	out, err := collect.ActivityLogCollector{}.Collect(context.Background(), collect.Input{
		ActivityLogPath:   path,
		TargetTopN:        25,
		StepTopN:          25,
		DecodeAttachments: true,
	})
	require.NoError(t, err)

	require.NotNil(t, out.AttachmentsCCM, "DecodeAttachments=true must produce a non-nil aggregate")
	assert.Positive(t, out.AttachmentsCCM.Attachments, "aggregate must count attachment-bearing sections")
	assert.Equal(t, int64(65), out.AttachmentsCCM.SwiftCacheHits, "swiftCacheHits round-trips through aggregator")
	assert.Equal(t, int64(0), out.AttachmentsCCM.SwiftCacheMisses, "fixture is cache-saturated")
}

func TestActivityLogCollector_MissingPath_NoOp(t *testing.T) {
	// Unresolvable DerivedData + nothing supplied — collector should not error.
	out, err := collect.ActivityLogCollector{}.Collect(context.Background(), collect.Input{})
	require.NoError(t, err)
	assert.Empty(t, out.Targets)
	assert.Nil(t, out.AttachmentsCCM)
}

func TestActivityLogCollector_TopNCap(t *testing.T) {
	path, err := filepath.Abs(slfFixture)
	require.NoError(t, err)

	out, err := collect.ActivityLogCollector{}.Collect(context.Background(), collect.Input{
		ActivityLogPath: path,
		TargetTopN:      1,
		StepTopN:        3,
	})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(out.Targets), 1, "TargetTopN caps the target list")
	assert.LessOrEqual(t, len(out.TopSteps), 3, "StepTopN caps the top-step list")
}
