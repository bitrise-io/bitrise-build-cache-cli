//go:build unit

package buildanalyzer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestClassify_FourBuckets covers every branch of reasonForRule in one table.
// Each case pins a Task against a Snapshot with a known iteration and asserts
// the ExecutionReason + BuiltAt + Signature derivation.
func TestClassify_FourBuckets(t *testing.T) {
	snap := Snapshot{
		Iteration: 10,
		Rules: map[string]RuleResult{
			// Command rules: build.db prefixes manifest keys with `C`.
			"CP0:::ran-this-build-rule": {
				Key:       "CP0:::ran-this-build-rule",
				BuiltAt:   time.Unix(10, 0).UTC(),
				Signature: []byte{0xDE, 0xAD, 0xBE, 0xEF},
			},
			"CP0:::skipped-rule": {
				Key:       "CP0:::skipped-rule",
				BuiltAt:   time.Unix(3, 0).UTC(),
				Signature: []byte{0x01, 0x02},
			},
			// Shouldn't happen in practice, but reasonForRule guards it.
			"CP0:::future-rule": {
				Key:       "CP0:::future-rule",
				BuiltAt:   time.Unix(42, 0).UTC(),
				Signature: []byte{0xAA},
			},
		},
	}

	report := &Report{
		Tasks: []Task{
			{Key: "ran-this-build-rule", ManifestKey: "P0:::ran-this-build-rule", Ran: true},
			{Key: "skipped-rule", ManifestKey: "P0:::skipped-rule", Ran: false},
			{Key: "future-rule", ManifestKey: "P0:::future-rule", Ran: true},
			{Key: "no-such-rule", ManifestKey: "P0:::no-such-rule", Ran: true},
		},
	}

	Classify(report, snap)

	require.Equal(t, ReasonRanThisBuild, report.Tasks[0].ExecutionReason)
	require.Equal(t, int64(10), report.Tasks[0].BuiltAt.Unix())
	require.Equal(t, "deadbeef", report.Tasks[0].Signature)

	require.Equal(t, ReasonSkippedUpToDate, report.Tasks[1].ExecutionReason)
	require.Equal(t, int64(3), report.Tasks[1].BuiltAt.Unix())
	require.Equal(t, "0102", report.Tasks[1].Signature)

	require.Equal(t, ReasonUnclassified, report.Tasks[2].ExecutionReason)

	require.Equal(t, ReasonCold, report.Tasks[3].ExecutionReason)
	require.True(t, report.Tasks[3].BuiltAt.IsZero())
	require.Empty(t, report.Tasks[3].Signature)
}

// TestClassify_IterationZeroFallback verifies the "snap.Iteration == 0" path
// where the activity-log Ran flag is the only signal; rules get RanThisBuild
// when Ran=true and SkippedUpToDate otherwise, and missing rows still trip
// ReasonCold.
func TestClassify_IterationZeroFallback(t *testing.T) {
	snap := Snapshot{
		Iteration: 0,
		Rules: map[string]RuleResult{
			"CA": {Key: "CA", BuiltAt: time.Time{}, Signature: []byte{0x01}},
			"CB": {Key: "CB", BuiltAt: time.Time{}, Signature: []byte{0x02}},
		},
	}

	report := &Report{
		Tasks: []Task{
			{Key: "A", ManifestKey: "A", Ran: true},
			{Key: "B", ManifestKey: "B", Ran: false},
			{Key: "C", ManifestKey: "C", Ran: true},
		},
	}

	Classify(report, snap)

	require.Equal(t, ReasonRanThisBuild, report.Tasks[0].ExecutionReason)
	require.Equal(t, ReasonSkippedUpToDate, report.Tasks[1].ExecutionReason)
	require.Equal(t, ReasonCold, report.Tasks[2].ExecutionReason)
}

// TestClassify_FallbackLookup ensures the three-tiered lookup (C+ManifestKey
// → ManifestKey → Key) gives the same match whichever path hits first.
func TestClassify_FallbackLookup(t *testing.T) {
	rule := RuleResult{Key: "X", BuiltAt: time.Unix(5, 0).UTC(), Signature: []byte{0xFF}}
	snap := Snapshot{
		Iteration: 5,
		Rules: map[string]RuleResult{
			"X": rule, // no `C` prefix — test the fallback path.
		},
	}

	report := &Report{
		Tasks: []Task{
			{Key: "X", ManifestKey: "X", Ran: true},
		},
	}

	Classify(report, snap)
	require.Equal(t, ReasonRanThisBuild, report.Tasks[0].ExecutionReason)
	require.Equal(t, "ff", report.Tasks[0].Signature)
}
