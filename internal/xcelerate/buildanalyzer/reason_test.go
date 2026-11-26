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
			"CP0:::B":   {"N/tmp/A.o"},
			"N/tmp/A.o": {"CP0:::A"},
			// C consumes NodeB produced by B.
			"CP0:::C":   {"N/tmp/B.o"},
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
			"CP0:::CompileFoo": {Key: "CP0:::CompileFoo", Iteration: 2, Signature: []byte{0xBB}, Value: []byte{0x02}},
		},
		PriorRules: map[string]RuleResult{
			"CP0:::CompileFoo": {Key: "CP0:::CompileFoo", Iteration: 1, Signature: []byte{0xAA}, Value: []byte{0x01}},
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

// TestClassify_ReEvaluatedCached_WhenRanIsFalse pins the new six-bucket
// branch: a rule whose iteration matches the snapshot's but whose Task has
// no activity-log section (Ran=false) reads as ReasonReEvaluatedCached —
// llbuild touched the rule but did not invoke the underlying command.
func TestClassify_ReEvaluatedCached_WhenRanIsFalse(t *testing.T) {
	snap := Snapshot{
		Iteration: 7,
		Rules: map[string]RuleResult{
			"CP0:::Touched": {Key: "CP0:::Touched", Iteration: 7, Signature: []byte{0x11}},
		},
	}

	report := &Report{
		Tasks: []Task{
			{Key: "Touched", ManifestKey: "P0:::Touched", Ran: false},
		},
	}

	Classify(report, snap)

	assert.Equal(t, ReasonReEvaluatedCached, report.Tasks[0].ExecutionReason)
	// The task did not run, so no upstream attribution is populated.
	assert.False(t, report.Tasks[0].RootCause)
	assert.Equal(t, 0, report.Tasks[0].UpstreamRanCount)
	assert.Empty(t, report.Tasks[0].ChangeCauseKeys)
}

// TestApplyHistoricalDiff_PopulatesValueHashes verifies the new
// PriorValueHex / CurrentValueHex derivation: on a signature-changed
// classification, both fields are populated with distinct 16-hex fingerprints
// and the key-identity PriorSignature is still carried.
func TestApplyHistoricalDiff_PopulatesValueHashes(t *testing.T) {
	snap := Snapshot{
		Iteration: 2,
		Rules: map[string]RuleResult{
			"CP0:::CompileFoo": {
				Key: "CP0:::CompileFoo", Iteration: 2,
				Signature: []byte{0xBB},
				Value:     []byte("current-result"),
			},
		},
		PriorRules: map[string]RuleResult{
			"CP0:::CompileFoo": {
				Key: "CP0:::CompileFoo", Iteration: 1,
				Signature: []byte{0xAA},
				Value:     []byte("prior-result"),
			},
		},
	}

	report := &Report{
		Tasks: []Task{{Key: "CompileFoo", ManifestKey: "P0:::CompileFoo", Ran: true}},
	}

	Classify(report, snap)

	task := report.Tasks[0]
	assert.True(t, task.SignatureChanged)
	assert.Equal(t, "aa", task.PriorSignature, "identity hash still exposed for backwards compat")
	assert.Len(t, task.PriorValueHex, 16)
	assert.Len(t, task.CurrentValueHex, 16)
	assert.NotEqual(t, task.PriorValueHex, task.CurrentValueHex, "distinct Value blobs → distinct hashes")
}

// TestFileChanges_PopulatesFromChangedNodeValues sets up a ran Command B
// consuming a Node whose Value differs between the prior and current
// snapshots. Expect Report.FileChanges to carry {NodeKey, B}.
func TestFileChanges_PopulatesFromChangedNodeValues(t *testing.T) {
	snap := Snapshot{
		Iteration: 5,
		Rules: map[string]RuleResult{
			"CP0:::Compile":   {Key: "CP0:::Compile", Iteration: 5, Value: []byte("compile-v2")},
			"N/src/Foo.swift": {Key: "N/src/Foo.swift", Iteration: 5, Value: []byte("content-v2")},
		},
		PriorRules: map[string]RuleResult{
			"CP0:::Compile":   {Key: "CP0:::Compile", Iteration: 4, Value: []byte("compile-v1")},
			"N/src/Foo.swift": {Key: "N/src/Foo.swift", Iteration: 4, Value: []byte("content-v1")},
		},
		Deps: map[string][]string{
			"CP0:::Compile": {"N/src/Foo.swift"},
		},
	}

	report := &Report{
		Tasks: []Task{{Key: "Compile", ManifestKey: "P0:::Compile", Ran: true}},
	}

	Classify(report, snap)

	require.Len(t, report.FileChanges, 1)
	assert.Equal(t, "N/src/Foo.swift", report.FileChanges[0].NodeKey)
	assert.Equal(t, "CP0:::Compile", report.FileChanges[0].DownstreamTaskKey)
}

// TestImmediateChangeCauses_SkipsHeadOfChain seeds a leaf whose direct
// upstream Commands are (a) a ClangStatCache (head-of-chain) and (b) a real
// ran compile. Expect ChangeCauseKeys to carry only the real compile, and
// the ClangStatCache to drop out of RootCauseKeys even though it has no ran
// upstream itself.
func TestImmediateChangeCauses_SkipsHeadOfChain(t *testing.T) {
	snap := Snapshot{
		Iteration: 3,
		Rules: map[string]RuleResult{
			"CP0:::ClangStatCache /tmp/cache": {Key: "CP0:::ClangStatCache /tmp/cache", Iteration: 3},
			"CP0:::CompileSwift Foo":          {Key: "CP0:::CompileSwift Foo", Iteration: 3},
			"CP0:::Leaf":                      {Key: "CP0:::Leaf", Iteration: 3},
		},
		Deps: map[string][]string{
			"CP0:::Leaf":             {"N/tmp/stat", "N/tmp/foo.o"},
			"N/tmp/stat":             {"CP0:::ClangStatCache /tmp/cache"},
			"N/tmp/foo.o":            {"CP0:::CompileSwift Foo"},
			"CP0:::CompileSwift Foo": {"N/src/foo.swift"},
		},
	}

	report := &Report{
		Tasks: []Task{
			{Key: "ClangStatCache /tmp/cache", ManifestKey: "P0:::ClangStatCache /tmp/cache", Ran: true},
			{Key: "CompileSwift Foo", ManifestKey: "P0:::CompileSwift Foo", Ran: true},
			{Key: "Leaf", ManifestKey: "P0:::Leaf", Ran: true},
		},
	}

	Classify(report, snap)

	leaf := report.Tasks[2]
	assert.Equal(t, []string{"CP0:::CompileSwift Foo"}, leaf.ChangeCauseKeys,
		"head-of-chain ClangStatCache filtered out")

	// ClangStatCache is RootCause at the Task level (UpstreamRanCount=0),
	// but excluded from the Report-level RootCauseKeys.
	assert.True(t, report.Tasks[0].RootCause)
	assert.NotContains(t, report.RootCauseKeys, "ClangStatCache /tmp/cache")
	assert.Contains(t, report.RootCauseKeys, "CompileSwift Foo")
}

// TestImmediateChangeCauses_WalksThroughGate wires A → Gate → B so B's
// direct upstream is a phony gate rule. Expect ChangeCauseKeys to surface
// A, not the gate.
func TestImmediateChangeCauses_WalksThroughGate(t *testing.T) {
	snap := Snapshot{
		Iteration: 2,
		Rules: map[string]RuleResult{
			"CP0:::A":                 {Key: "CP0:::A", Iteration: 2},
			"CP0:::Gate target-X:all": {Key: "CP0:::Gate target-X:all", Iteration: 2},
			"CP0:::B":                 {Key: "CP0:::B", Iteration: 2},
		},
		Deps: map[string][]string{
			"CP0:::B":                 {"N/virtual/gate"},
			"N/virtual/gate":          {"CP0:::Gate target-X:all"},
			"CP0:::Gate target-X:all": {"N/tmp/a.o"},
			"N/tmp/a.o":               {"CP0:::A"},
		},
	}

	report := &Report{
		Tasks: []Task{
			{Key: "A", ManifestKey: "P0:::A", Ran: true},
			{Key: "Gate target-X:all", ManifestKey: "P0:::Gate target-X:all", Ran: true},
			{Key: "B", ManifestKey: "P0:::B", Ran: true},
		},
	}

	Classify(report, snap)

	// B's direct upstream is the gate; we recurse through it to find A.
	assert.Equal(t, []string{"CP0:::A"}, report.Tasks[2].ChangeCauseKeys,
		"gate rule stepped past, real Command A surfaced")
}

// TestImmediateChangeCauses_FallsThroughToNearestRanAncestor sets up
// A (ran) → N → B (skipped) → N → C (ran). C's direct upstream (B) is
// skipped so direct one-hop returns nothing — the transitive walk finds A
// as the nearest ran ancestor. ChangeCauseKeys should surface A, not the
// empty list.
func TestImmediateChangeCauses_FallsThroughToNearestRanAncestor(t *testing.T) {
	snap := Snapshot{
		Iteration: 10,
		Rules: map[string]RuleResult{
			"CP0:::A": {Key: "CP0:::A", Iteration: 10}, // ran
			"CP0:::B": {Key: "CP0:::B", Iteration: 3},  // skipped
			"CP0:::C": {Key: "CP0:::C", Iteration: 10}, // ran
		},
		Deps: map[string][]string{
			"CP0:::C":   {"N/tmp/B.o"},
			"N/tmp/B.o": {"CP0:::B"},
			"CP0:::B":   {"N/tmp/A.o"},
			"N/tmp/A.o": {"CP0:::A"},
		},
	}

	report := &Report{
		Tasks: []Task{
			{Key: "A", ManifestKey: "P0:::A", Ran: true},
			{Key: "B", ManifestKey: "P0:::B", Ran: false},
			{Key: "C", ManifestKey: "P0:::C", Ran: true},
		},
	}

	Classify(report, snap)

	c := report.Tasks[2]
	assert.Equal(t, ReasonRanThisBuild, c.ExecutionReason)
	assert.Equal(t, 1, c.UpstreamRanCount, "A is the only ran ancestor")
	assert.Equal(t, []string{"CP0:::A"}, c.ChangeCauseKeys,
		"nearest ran ancestor surfaced when direct upstream is skipped")
}

// TestApplyHistoricalDiff_PopulatesByteLevelFields seeds two Value BLOBs
// that differ at a known offset and asserts the A-level fields
// (PriorValueBytesHex / CurrentValueBytesHex / ValueDiffOffset /
// ValueDiffNote) all land with the correct values.
func TestApplyHistoricalDiff_PopulatesByteLevelFields(t *testing.T) {
	// First 4 bytes shared, byte 4 flips 0xAA → 0xBB.
	prior := []byte{0x01, 0x02, 0x03, 0x04, 0xAA, 0x00, 0x00, 0x00}
	current := []byte{0x01, 0x02, 0x03, 0x04, 0xBB, 0x00, 0x00, 0x00}

	snap := Snapshot{
		Iteration: 2,
		Rules: map[string]RuleResult{
			"CP0:::CompileFoo": {Key: "CP0:::CompileFoo", Iteration: 2, Value: current},
		},
		PriorRules: map[string]RuleResult{
			"CP0:::CompileFoo": {Key: "CP0:::CompileFoo", Iteration: 1, Value: prior},
		},
	}

	report := &Report{
		Tasks: []Task{{Key: "CompileFoo", ManifestKey: "P0:::CompileFoo", Ran: true}},
	}

	Classify(report, snap)

	task := report.Tasks[0]
	require.True(t, task.SignatureChanged)
	assert.Equal(t, "01020304aa000000", task.PriorValueBytesHex)
	assert.Equal(t, "01020304bb000000", task.CurrentValueBytesHex)
	assert.Equal(t, 4, task.ValueDiffOffset, "first byte that differs is at offset 4")
	assert.Contains(t, task.ValueDiffNote, "byte 4")
	assert.Contains(t, task.ValueDiffNote, "0xaa")
	assert.Contains(t, task.ValueDiffNote, "0xbb")
}

// TestFileChanges_PopulatesFileInfoDecode seeds real-shape Node Value
// BLOBs (the ImageTask.swift fixture) before + after a size change and
// asserts the resulting FileChange carries a semantic FileInfo decode
// plus a byte-level diff.
func TestFileChanges_PopulatesFileInfoDecode(t *testing.T) {
	prior := mustHex(t, imageTaskPriorHex)
	current := mustHex(t, imageTaskCurrentHex)

	snap := Snapshot{
		Iteration: 5,
		Rules: map[string]RuleResult{
			"CP0:::Compile":                     {Key: "CP0:::Compile", Iteration: 5, Value: []byte("compile-v2")},
			"N/Users/x/dev/Nuke/ImageTask.swift": {Key: "N/Users/x/dev/Nuke/ImageTask.swift", Iteration: 5, Value: current},
		},
		PriorRules: map[string]RuleResult{
			"CP0:::Compile":                     {Key: "CP0:::Compile", Iteration: 4, Value: []byte("compile-v1")},
			"N/Users/x/dev/Nuke/ImageTask.swift": {Key: "N/Users/x/dev/Nuke/ImageTask.swift", Iteration: 4, Value: prior},
		},
		Deps: map[string][]string{
			"CP0:::Compile": {"N/Users/x/dev/Nuke/ImageTask.swift"},
		},
	}

	report := &Report{
		Tasks: []Task{{Key: "Compile", ManifestKey: "P0:::Compile", Ran: true}},
	}

	Classify(report, snap)

	require.Len(t, report.FileChanges, 1)
	fc := report.FileChanges[0]
	assert.Equal(t, "N/Users/x/dev/Nuke/ImageTask.swift", fc.NodeKey)

	require.NotNil(t, fc.PriorFile, "prior FileInfo decoded")
	require.NotNil(t, fc.CurrentFile, "current FileInfo decoded")
	assert.NotEqual(t, fc.PriorFile.Size, fc.CurrentFile.Size,
		"semantic size differs after the mutation")
	assert.Equal(t, uint64(21099), fc.PriorFile.Size)
	assert.Equal(t, uint64(21121), fc.CurrentFile.Size)

	assert.Contains(t, fc.SemanticDiff, "size: 21099 → 21121")

	// A-level fields populated regardless of B-decode success.
	assert.NotEmpty(t, fc.PriorValueBytesHex)
	assert.NotEmpty(t, fc.CurrentValueBytesHex)
	// Size field lives at byte 29 (little-endian uint64) — first flipped
	// byte in the fixture is the size low byte at that offset.
	assert.Equal(t, 29, fc.ValueDiffOffset)
	assert.Contains(t, fc.ValueDiffNote, "byte 29")
}
