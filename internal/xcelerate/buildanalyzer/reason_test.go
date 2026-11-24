//go:build unit

package buildanalyzer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClassify_FourBuckets covers every branch of reasonForRule in one table.
// Each case pins a Task against a Snapshot with a known iteration and asserts
// the ExecutionReason + Iteration + Signature derivation.
func TestClassify_FourBuckets(t *testing.T) {
	snap := Snapshot{
		Iteration: 10,
		Rules: map[string]RuleResult{
			"CP0:::ran-this-build-rule": {
				Key:       "CP0:::ran-this-build-rule",
				Iteration: 10,
				Signature: []byte{0xDE, 0xAD, 0xBE, 0xEF},
			},
			"CP0:::skipped-rule": {
				Key:       "CP0:::skipped-rule",
				Iteration: 3,
				Signature: []byte{0x01, 0x02},
			},
			"CP0:::future-rule": {
				Key:       "CP0:::future-rule",
				Iteration: 42,
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
	require.Equal(t, int64(10), report.Tasks[0].Iteration)
	require.Equal(t, "deadbeef", report.Tasks[0].Signature)

	require.Equal(t, ReasonSkippedUpToDate, report.Tasks[1].ExecutionReason)
	require.Equal(t, int64(3), report.Tasks[1].Iteration)
	require.Equal(t, "0102", report.Tasks[1].Signature)

	require.Equal(t, ReasonUnclassified, report.Tasks[2].ExecutionReason)

	require.Equal(t, ReasonCold, report.Tasks[3].ExecutionReason)
	require.Equal(t, int64(0), report.Tasks[3].Iteration)
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
			"CA": {Key: "CA", Signature: []byte{0x01}},
			"CB": {Key: "CB", Signature: []byte{0x02}},
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

// TestClassify_PopulatesAttribution_RootCauseAndUpstreamCount seeds three
// tasks where A is a root cause (ran, no ran upstream), B and C depend on A
// via Node intermediates and also ran — so they should carry
// UpstreamRanCount=1 and A should show up in Report.RootCauseKeys.
func TestClassify_PopulatesAttribution_RootCauseAndUpstreamCount(t *testing.T) {
	snap := Snapshot{
		Iteration: 7,
		Rules: map[string]RuleResult{
			"CP0:::A": {Key: "CP0:::A", Iteration: 7, Signature: []byte{0x11}},
			"CP0:::B": {Key: "CP0:::B", Iteration: 7, Signature: []byte{0x22}},
			"CP0:::C": {Key: "CP0:::C", Iteration: 7, Signature: []byte{0x33}},
		},
		Deps: map[string][]string{
			// B consumes NodeA produced by A.
			"CP0:::B": {"N/tmp/A.o"},
			"N/tmp/A.o": {"CP0:::A"},
			// C consumes NodeB produced by B.
			"CP0:::C": {"N/tmp/B.o"},
			"N/tmp/B.o": {"CP0:::B"},
		},
	}

	report := &Report{
		Tasks: []Task{
			{Key: "A", ManifestKey: "P0:::A", Ran: true},
			{Key: "B", ManifestKey: "P0:::B", Ran: true},
			{Key: "C", ManifestKey: "P0:::C", Ran: true},
		},
	}

	Classify(report, snap)

	require.Equal(t, ReasonRanThisBuild, report.Tasks[0].ExecutionReason)
	require.True(t, report.Tasks[0].RootCause, "A has no ran upstream — root cause")
	require.Equal(t, 0, report.Tasks[0].UpstreamRanCount)
	require.Empty(t, report.Tasks[0].ChangeCauseKeys)

	require.Equal(t, ReasonRanThisBuild, report.Tasks[1].ExecutionReason)
	require.False(t, report.Tasks[1].RootCause)
	require.Equal(t, 1, report.Tasks[1].UpstreamRanCount, "B has A as transitive ran upstream")
	require.Equal(t, []string{"CP0:::A"}, report.Tasks[1].ChangeCauseKeys)

	require.Equal(t, ReasonRanThisBuild, report.Tasks[2].ExecutionReason)
	require.False(t, report.Tasks[2].RootCause)
	require.Equal(t, 2, report.Tasks[2].UpstreamRanCount, "C has B and A transitively")
	require.Equal(t, []string{"CP0:::B"}, report.Tasks[2].ChangeCauseKeys, "direct cause only, not transitive")

	require.Equal(t, []string{"A"}, report.RootCauseKeys)
}

// TestClassify_ChangeCauseKeysTruncation drives the >10-entries path so the
// `…` sentinel lands and the surviving prefix is kept intact.
func TestClassify_ChangeCauseKeysTruncation(t *testing.T) {
	// 12 ran upstream Commands directly feeding one leaf. After the cap we
	// expect 10 keys + one sentinel.
	deps := map[string][]string{}
	rules := map[string]RuleResult{
		"CP0:::leaf": {Key: "CP0:::leaf", Iteration: 1},
	}

	leafDeps := []string{}
	for i := 0; i < 12; i++ {
		nodeKey := nodeKeyForTest(i)
		cmdKey := commandKeyForTest(i)

		leafDeps = append(leafDeps, nodeKey)
		deps[nodeKey] = []string{cmdKey}
		rules[cmdKey] = RuleResult{Key: cmdKey, Iteration: 1}
	}
	deps["CP0:::leaf"] = leafDeps

	snap := Snapshot{Iteration: 1, Rules: rules, Deps: deps}

	report := &Report{
		Tasks: []Task{{Key: "leaf", ManifestKey: "P0:::leaf", Ran: true}},
	}

	Classify(report, snap)

	causes := report.Tasks[0].ChangeCauseKeys
	require.Len(t, causes, 11, "10 entries + 1 sentinel")
	require.Equal(t, "…", causes[len(causes)-1])
}

// nodeKeyForTest / commandKeyForTest generate unique keys with the right
// prefix tag so the walker treats them as Node / Command. Kept trivial.
func nodeKeyForTest(i int) string {
	return "N/tmp/node" + itoaForTest(i) + ".o"
}

func commandKeyForTest(i int) string {
	return "CP0:::Cmd" + itoaForTest(i)
}

func itoaForTest(i int) string {
	// avoid strconv import in this small helper
	if i == 0 {
		return "0"
	}
	const digits = "0123456789"
	var buf [8]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = digits[i%10]
		i /= 10
	}
	return string(buf[pos:])
}

// TestClassify_PriorSignatureDiff_DetectsChange verifies B.2's signature
// change signal: a current rule with signature BB vs a prior rule with
// signature AA under the same key sets SignatureChanged + PriorSignature.
func TestClassify_PriorSignatureDiff_DetectsChange(t *testing.T) {
	snap := Snapshot{
		Iteration: 2,
		Rules: map[string]RuleResult{
			"CP0:::CompileFoo": {Key: "CP0:::CompileFoo", Iteration: 2, Signature: []byte{0xBB}},
		},
		PriorRules: map[string]RuleResult{
			"CP0:::CompileFoo": {Key: "CP0:::CompileFoo", Iteration: 1, Signature: []byte{0xAA}},
		},
	}

	report := &Report{
		Tasks: []Task{{Key: "CompileFoo", ManifestKey: "P0:::CompileFoo", Ran: true}},
	}

	Classify(report, snap)

	assert.True(t, report.Tasks[0].SignatureChanged)
	assert.Equal(t, "aa", report.Tasks[0].PriorSignature)
	assert.Equal(t, "bb", report.Tasks[0].Signature)
	// Signature-change is orthogonal to the iteration-based bucket; iter=2
	// matches snap.Iteration so the row still reads as ran-this-build.
	assert.Equal(t, ReasonRanThisBuild, report.Tasks[0].ExecutionReason)
}

// TestClassify_PriorSignatureDiff_NewTaskPromotion asserts that a
// ran-this-build task with no matching prior rule is promoted to
// ReasonNewTask so operators can tell "first-run" from "re-run".
func TestClassify_PriorSignatureDiff_NewTaskPromotion(t *testing.T) {
	snap := Snapshot{
		Iteration: 2,
		Rules: map[string]RuleResult{
			"CP0:::BrandNew": {Key: "CP0:::BrandNew", Iteration: 2, Signature: []byte{0x42}},
		},
		PriorRules: map[string]RuleResult{
			// Prior snapshot doesn't carry this key at all.
			"CP0:::Unrelated": {Key: "CP0:::Unrelated", Iteration: 1, Signature: []byte{0x01}},
		},
	}

	report := &Report{
		Tasks: []Task{{Key: "BrandNew", ManifestKey: "P0:::BrandNew", Ran: true}},
	}

	Classify(report, snap)

	assert.Equal(t, ReasonNewTask, report.Tasks[0].ExecutionReason)
	assert.False(t, report.Tasks[0].SignatureChanged, "new task has no prior sig to flag")
	assert.Empty(t, report.Tasks[0].PriorSignature)
}

// TestClassify_PriorSignatureDiff_UnchangedKeepsFlags asserts the inert path:
// current and prior signatures match → SignatureChanged stays false, no
// PriorSignature exposed, ExecutionReason unchanged.
func TestClassify_PriorSignatureDiff_UnchangedKeepsFlags(t *testing.T) {
	snap := Snapshot{
		Iteration: 2,
		Rules: map[string]RuleResult{
			"CP0:::Stable": {Key: "CP0:::Stable", Iteration: 2, Signature: []byte{0xAA, 0xBB}},
		},
		PriorRules: map[string]RuleResult{
			"CP0:::Stable": {Key: "CP0:::Stable", Iteration: 1, Signature: []byte{0xAA, 0xBB}},
		},
	}

	report := &Report{
		Tasks: []Task{{Key: "Stable", ManifestKey: "P0:::Stable", Ran: true}},
	}

	Classify(report, snap)

	assert.False(t, report.Tasks[0].SignatureChanged)
	assert.Empty(t, report.Tasks[0].PriorSignature)
	assert.Equal(t, ReasonRanThisBuild, report.Tasks[0].ExecutionReason)
}

// TestClassify_NoPriorRulesPreservesSingleSnapshotBehavior ensures Classify
// stays idempotent against the pre-B.2 shape when PriorRules is nil —
// SignatureChanged / PriorSignature never leak into the output, and
// ExecutionReason mirrors the four-bucket contract.
func TestClassify_NoPriorRulesPreservesSingleSnapshotBehavior(t *testing.T) {
	snap := Snapshot{
		Iteration: 2,
		Rules: map[string]RuleResult{
			"CP0:::Foo": {Key: "CP0:::Foo", Iteration: 2, Signature: []byte{0xAA}},
		},
		// PriorRules intentionally nil.
	}

	report := &Report{
		Tasks: []Task{{Key: "Foo", ManifestKey: "P0:::Foo", Ran: true}},
	}

	Classify(report, snap)

	assert.Equal(t, ReasonRanThisBuild, report.Tasks[0].ExecutionReason)
	assert.False(t, report.Tasks[0].SignatureChanged)
	assert.Empty(t, report.Tasks[0].PriorSignature)
}

// TestClassify_FallbackLookup ensures the three-tiered lookup (C+ManifestKey
// → ManifestKey → Key) gives the same match whichever path hits first.
func TestClassify_FallbackLookup(t *testing.T) {
	rule := RuleResult{Key: "X", Iteration: 5, Signature: []byte{0xFF}}
	snap := Snapshot{
		Iteration: 5,
		Rules: map[string]RuleResult{
			"X": rule,
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
