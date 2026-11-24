package buildanalyzer

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// changeCauseKeysLimit caps Task.ChangeCauseKeys entries so pretty / JSON
// output stays scannable; a sentinel "…" takes the final slot when truncated.
const changeCauseKeysLimit = 10

// gateWalkHopCap bounds the one-level recursion through llbuild Gate rules
// inside immediateChangeCauses — a safety net against pathological graphs
// where gates chain into further gates. Three hops is well past anything
// observed in real Xcode manifests.
const gateWalkHopCap = 3

// ExecutionReason is the v2 bucket a task falls into once joined against the
// build.db Snapshot. The six values extend the original four-bucket split
// with B.2's `new-task` promotion and the follow-up `re-evaluated-cached`
// branch introduced to tell "llbuild re-checked this rule" from "llbuild
// actually invoked the command".
type ExecutionReason string

const (
	// ReasonCold — no rule_results row for this task in the current build.db.
	// Either the task is brand new or the DB has been wiped (clean build).
	ReasonCold ExecutionReason = "cold"
	// ReasonRanThisBuild — the task's rule row carries the current build's
	// iteration counter AND the activity log confirmed the command actually
	// executed. Set only when both signals line up.
	ReasonRanThisBuild ExecutionReason = "ran-this-build"
	// ReasonReEvaluatedCached — the task's rule row carries the current
	// build's iteration (llbuild re-checked inputs, saw they were valid) but
	// the activity log has no section for it (no process was spawned, the
	// prior result was reused). Separates "we looked at this rule" from "we
	// ran its command" — important on warm builds where llbuild touches
	// hundreds of rules but only a handful produce work.
	ReasonReEvaluatedCached ExecutionReason = "re-evaluated-cached"
	// ReasonSkippedUpToDate — the task's rule row exists but carries an older
	// iteration, meaning llbuild reused the prior result. These are the
	// "incremental skipped" tasks that make warm builds fast.
	ReasonSkippedUpToDate ExecutionReason = "skipped-up-to-date"
	// ReasonUnclassified — defensive fall-through when neither the iteration
	// comparison nor the time windowing lands cleanly. A non-zero count on a
	// real build points at a matching-key gap to chase.
	ReasonUnclassified ExecutionReason = "unclassified"
	// ReasonNewTask — the task appears in the current snapshot but had no
	// matching rule in the prior one. Populated only when Snapshot.PriorRules
	// is available (B.2's historical path). Refines what would otherwise
	// collapse into ReasonCold (no current row) or ReasonRanThisBuild (row
	// matched but we didn't know it was brand-new).
	ReasonNewTask ExecutionReason = "new-task"
)

// Classify walks every Task on report and sets ExecutionReason + Iteration +
// Signature from the Snapshot. Lookup is three-tiered, mirroring the match
// order the manifest join uses:
//
//  1. snap.Rules["C" + task.ManifestKey]  — the llbuild Command rule-kind
//     prefix the build.db wraps around manifest keys.
//  2. snap.Rules[task.ManifestKey]        — in case the prefix was not applied
//     (tests, older schema).
//  3. snap.Rules[task.Key]                — activity-log-only fallback.
//
// Classification compares rule.Iteration against snap.Iteration:
//   - no row                                        → ReasonCold
//   - rule.Iteration == snap.Iteration && Ran       → ReasonRanThisBuild
//   - rule.Iteration == snap.Iteration && !Ran      → ReasonReEvaluatedCached
//   - rule.Iteration < snap.Iteration               → ReasonSkippedUpToDate
//   - rule.Iteration > snap.Iteration               → ReasonUnclassified
//
// When snap.Iteration is zero (e.g. the info row was empty) we fall back to
// activity-log Ran as the Ran/Skipped tiebreaker so warm-build output stays
// usable; the cold bucket still trips cleanly on missing rows.
func Classify(report *Report, snap Snapshot) {
	if report == nil {
		return
	}

	for i := range report.Tasks {
		task := &report.Tasks[i]

		rule, matchedKey, found := lookupRule(snap.Rules, task)
		if !found {
			task.ExecutionReason = ReasonCold

			continue
		}

		task.Iteration = rule.Iteration
		if len(rule.Signature) > 0 {
			task.Signature = hex.EncodeToString(rule.Signature)
		}

		task.ExecutionReason = reasonForRule(rule, snap.Iteration, task.Ran)

		if task.ExecutionReason == ReasonRanThisBuild {
			attributeRanTask(task, matchedKey, snap)
		}

		applyHistoricalDiff(task, rule, matchedKey, snap)
	}

	collectFileChanges(report, snap)
	report.RootCauseKeys = collectRootCauseKeys(report.Tasks)
}

// applyHistoricalDiff folds in the prior-snapshot signal when it exists.
// No-op when snap.PriorRules is nil, preserving the single-snapshot behavior.
//
// Two orthogonal signals:
//   - signature change — the rule existed in the prior snapshot with a
//     different serialized Value than the current. Sets SignatureChanged +
//     PriorValueHex + CurrentValueHex so the operator can tell "an input to
//     this rule flipped between builds" from "something upstream re-ran".
//     Doesn't override ExecutionReason — a SignatureChanged task is still
//     ran-this-build / skipped / cold on the iteration-comparison dimension.
//   - new task — the rule is new to the current snapshot. Promotes a
//     ran-this-build classification to new-task so the "first-run" bucket
//     surfaces separately in UI / aggregates. Preserves cold / skipped /
//     unclassified rulings since those carry a stronger signal.
func applyHistoricalDiff(task *Task, rule RuleResult, matchedKey string, snap Snapshot) {
	if snap.PriorRules == nil {
		return
	}

	priorRule, priorFound := lookupPriorRule(snap.PriorRules, matchedKey, task)
	if !priorFound {
		if task.ExecutionReason == ReasonRanThisBuild {
			task.ExecutionReason = ReasonNewTask
		}

		return
	}

	// llbuild's rule_results.signature column is the key-identity hash
	// (stable for a given rule name) rather than an output-content hash, so
	// signature-vs-signature comparison never flags a real change. The
	// `value` BLOB carries the serialized rule result (file-stat snapshot,
	// command output metadata, etc.) and DOES differ when the result moves.
	// Diff the value bytes; expose two 16-hex-char fingerprints so the UI
	// can render `prior=<hex> now=<hex>` without needing the raw BLOB.
	// PriorSignature is kept populated with the identity hash for backwards
	// compat but docstring warns it is stable per rule key.
	if !bytesEqual(priorRule.Value, rule.Value) {
		task.SignatureChanged = true
		if len(priorRule.Signature) > 0 {
			task.PriorSignature = hex.EncodeToString(priorRule.Signature)
		}

		task.PriorValueHex = valueHashHex(priorRule.Value)
		task.CurrentValueHex = valueHashHex(rule.Value)
	}
}

// valueHashHex derives a 16-hex-character fingerprint of an llbuild rule
// Value BLOB using sha256. The BLOB itself can be hundreds of bytes (file-
// stat arrays, command output metadata) — the first 8 bytes of the digest
// are more than enough to tell two distinct values apart in a UI column.
// Empty input returns the empty string so the omitempty JSON tag hides the
// field when nothing to report.
func valueHashHex(value []byte) string {
	if len(value) == 0 {
		return ""
	}

	sum := sha256.Sum256(value)

	return hex.EncodeToString(sum[:8])
}

// bytesEqual tests byte-slice equality with the "unknown-side treated as
// equal" short-circuit — an empty slice on either side means "we have no
// comparison data", NOT "changed". Used by applyHistoricalDiff on the rule
// Value blobs so a cold-rule vs. populated-rule pair doesn't flip a false
// SignatureChanged. For the file-change path that already proves both
// sides are populated, bytesEqualExact drops the carve-out.
func bytesEqual(a, b []byte) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}

	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

// lookupPriorRule tries the matchedKey first (what the current lookup picked)
// then walks the same fallback chain lookupRule uses — so a rule whose
// prior-snapshot key shape differed from the current (shouldn't happen in
// practice but trivial to handle) still matches.
func lookupPriorRule(priors map[string]RuleResult, matchedKey string, task *Task) (RuleResult, bool) {
	if matchedKey != "" {
		if rule, ok := priors[matchedKey]; ok {
			return rule, true
		}
	}

	rule, _, ok := lookupRule(priors, task)

	return rule, ok
}

// attributeRanTask fills UpstreamRanCount, RootCause and ChangeCauseKeys for
// a task classified as ran-this-build. No-op when Snapshot has no Deps map
// (e.g. an older DB schema the reader soft-failed on) so the flags stay off
// and the JSON omitempty hides the fields — same contract as missing manifest
// rows.
func attributeRanTask(task *Task, matchedKey string, snap Snapshot) {
	if snap.Deps == nil {
		return
	}

	upstreamRan := UpstreamRanCommands(snap, matchedKey)
	task.UpstreamRanCount = len(upstreamRan)
	// RootCause is a `task has no ran upstream` signal — head-of-chain
	// Commands like ClangStatCache run on every build and flooded the
	// root-cause set with false positives; collectRootCauseKeys filters
	// them out at the Report level so we keep the Task-level flag honest
	// even if the aggregate hides it.
	task.RootCause = task.UpstreamRanCount == 0

	task.ChangeCauseKeys = immediateChangeCauses(snap, matchedKey)
}

// immediateChangeCauses walks the one-hop upstream Commands and keeps those
// that ran this build. Head-of-chain rules (ClangStatCache & co.) are
// filtered out so they don't show up as the cause of every compile. Gate
// rules — phony aggregation nodes llbuild inserts between targets — are
// transparent: when a direct upstream is a Gate we recurse through it (up
// to gateWalkHopCap hops) to its real Command upstream, dedup, and emit
// those instead. When the direct scan yields nothing but UpstreamRanCount
// > 0 the ran set is one hop further up the graph; nearestRanAncestors
// walks the transitive adjacency and surfaces the closest ran Commands so
// ChangeCauseKeys never comes back empty while UpstreamRanCount > 0.
// Truncated at changeCauseKeysLimit with a `…` sentinel.
func immediateChangeCauses(snap Snapshot, matchedKey string) []string {
	direct := DirectCommandDeps(snap, matchedKey)

	causes := collectRanChangeCauses(snap, direct, 0)

	if len(causes) == 0 {
		// Direct one-hop walk found no ran Commands. If a transitive walk
		// shows ran ancestors exist, surface the nearest ones so the field
		// stays consistent with UpstreamRanCount.
		if hasRanTransitiveAncestor(snap, matchedKey) {
			causes = nearestRanAncestors(snap, matchedKey)
		}
	}

	if len(causes) == 0 {
		return nil
	}

	sort.Strings(causes)

	if len(causes) > changeCauseKeysLimit {
		truncated := make([]string, 0, changeCauseKeysLimit+1)
		truncated = append(truncated, causes[:changeCauseKeysLimit]...)
		truncated = append(truncated, "…")

		return truncated
	}

	return causes
}

// collectRanChangeCauses turns the direct-upstream Command list into the
// deduped ran subset, stepping through Gate commands transparently up to
// depth hops deep. A ran, non-gate Command is emitted; a Gate — regardless
// of its own ran state — expands to its own direct Command upstream and we
// recurse one hop further. The depth guard stops infinite walks on
// pathological graphs.
func collectRanChangeCauses(snap Snapshot, direct []string, depth int) []string {
	if len(direct) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(direct))
	out := make([]string, 0, len(direct))

	for _, cmd := range direct {
		if isGateKey(cmd) {
			if depth >= gateWalkHopCap {
				continue
			}

			for _, inner := range collectRanChangeCauses(snap, DirectCommandDeps(snap, cmd), depth+1) {
				if _, dup := seen[inner]; dup {
					continue
				}

				seen[inner] = struct{}{}
				out = append(out, inner)
			}

			continue
		}

		if isHeadOfChain(cmd) {
			continue
		}

		rule, ok := snap.Rules[cmd]
		if !ok {
			continue
		}

		if !ruleRanThisBuild(rule, snap.Iteration) {
			continue
		}

		if _, dup := seen[cmd]; dup {
			continue
		}

		seen[cmd] = struct{}{}
		out = append(out, cmd)
	}

	return out
}

// hasRanTransitiveAncestor returns true when ANY transitively-upstream
// Command of matchedKey ran this build. Used to decide whether an empty
// direct-walk result is worth replacing with the nearest-ran-ancestor
// fallback — on a task with no ran ancestors at all we don't invent one.
func hasRanTransitiveAncestor(snap Snapshot, matchedKey string) bool {
	return len(UpstreamRanCommands(snap, matchedKey)) > 0
}

// nearestRanAncestors runs a BFS through Snapshot.Deps starting at matchedKey
// and returns the FIRST layer of ran Commands reached — i.e. skipped
// intermediaries are walked past and only the frontier of ran ancestors is
// emitted. Head-of-chain + gate rules are transparent (gates recursed through,
// head-of-chain filtered). Guaranteed to return a bounded set thanks to the
// visited guard; callers truncate at changeCauseKeysLimit.
func nearestRanAncestors(snap Snapshot, matchedKey string) []string {
	if snap.Deps == nil {
		return nil
	}

	visited := map[string]struct{}{matchedKey: {}}
	frontier := DirectCommandDeps(snap, matchedKey)
	found := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)

	for len(frontier) > 0 {
		next := make([]string, 0, len(frontier)*2)

		for _, cmd := range frontier {
			if _, dup := visited[cmd]; dup {
				continue
			}

			visited[cmd] = struct{}{}

			if isGateKey(cmd) {
				next = append(next, DirectCommandDeps(snap, cmd)...)

				continue
			}

			if isHeadOfChain(cmd) {
				// Continue walking past the head-of-chain to see if a ran
				// Command sits upstream of it that actually explains the
				// change.
				next = append(next, DirectCommandDeps(snap, cmd)...)

				continue
			}

			rule, ok := snap.Rules[cmd]
			if !ok {
				next = append(next, DirectCommandDeps(snap, cmd)...)

				continue
			}

			if ruleRanThisBuild(rule, snap.Iteration) {
				if _, dup := seen[cmd]; !dup {
					seen[cmd] = struct{}{}
					found = append(found, cmd)
				}

				continue
			}

			// Not ran — walk further upstream.
			next = append(next, DirectCommandDeps(snap, cmd)...)
		}

		if len(found) > 0 {
			// Stop at the first frontier that yields at least one ran
			// ancestor — that's the NEAREST layer by definition.
			break
		}

		frontier = next
	}

	return found
}

// collectFileChanges surfaces changed input file Nodes as first-class
// FileChange entries. llbuild stores a Value BLOB on every Node rule
// (stat snapshot / content hash); diffing current vs prior Values catches
// the source file(s) that actually tripped a downstream Command to re-run.
// Without this, Command-level sig-diffs show a `CompileSwift` row flipping
// but never name the `.swift` file that changed — the operator has to open
// the DB manually.
//
// No-op when PriorRules is nil (single-snapshot mode).
func collectFileChanges(report *Report, snap Snapshot) {
	if snap.PriorRules == nil || snap.Deps == nil {
		return
	}

	type key struct {
		node, downstream string
	}

	seen := make(map[key]struct{}, 16)
	out := make([]FileChange, 0, 16)

	for i := range report.Tasks {
		task := &report.Tasks[i]
		if !task.SignatureChanged {
			continue
		}

		if task.ExecutionReason != ReasonRanThisBuild {
			continue
		}

		_, matchedKey, ok := lookupRule(snap.Rules, task)
		if !ok {
			continue
		}

		for _, node := range nodeInputsForCommand(snap, matchedKey) {
			if nodeValueChanged(snap, node) {
				k := key{node: node, downstream: matchedKey}
				if _, dup := seen[k]; dup {
					continue
				}

				seen[k] = struct{}{}
				out = append(out, FileChange{NodeKey: node, DownstreamTaskKey: matchedKey})
			}
		}
	}

	if len(out) == 0 {
		return
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].NodeKey != out[j].NodeKey {
			return out[i].NodeKey < out[j].NodeKey
		}

		return out[i].DownstreamTaskKey < out[j].DownstreamTaskKey
	})

	report.FileChanges = out
}

// nodeInputsForCommand returns the direct Node dependencies of cmdKey —
// one-hop walk that stops at the Node, no further walk into producers.
func nodeInputsForCommand(snap Snapshot, cmdKey string) []string {
	deps := snap.Deps[cmdKey]
	if len(deps) == 0 {
		return nil
	}

	out := make([]string, 0, len(deps))
	for _, dep := range deps {
		if isNodeKey(dep) {
			out = append(out, dep)
		}
	}

	return out
}

// nodeValueChanged returns true when the Node rule's current Value differs
// from the prior snapshot's. Missing either side yields false — we don't
// invent a change from "unknown".
func nodeValueChanged(snap Snapshot, nodeKey string) bool {
	current, okCur := snap.Rules[nodeKey]
	prior, okPrior := snap.PriorRules[nodeKey]

	if !okCur || !okPrior {
		return false
	}

	if len(current.Value) == 0 || len(prior.Value) == 0 {
		return false
	}

	return !bytesEqualExact(current.Value, prior.Value)
}

// bytesEqualExact is bytesEqual without the "empty means equal" carve-out.
// Used by collectFileChanges where we've already proved both Values are
// populated — any difference should be reported.
func bytesEqualExact(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

// collectRootCauseKeys scans the classified task list and returns the sorted
// unique set of Task.Key values where RootCause is set, EXCLUDING any key
// that matches the head-of-chain carve-out (see isHeadOfChain). Head-of-
// chain rules like ClangStatCache run on every build and make up ~every
// root-cause slot on a real monorepo — surfacing them tells the operator
// nothing. Sorting keeps JSON diffs stable between runs on the same DB.
func collectRootCauseKeys(tasks []Task) []string {
	if len(tasks) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, 16)
	for _, task := range tasks {
		if !task.RootCause {
			continue
		}

		if isHeadOfChain(task.Key) || isHeadOfChain(task.ManifestKey) {
			continue
		}

		if _, ok := seen[task.Key]; ok {
			continue
		}

		seen[task.Key] = struct{}{}
	}

	if len(seen) == 0 {
		return nil
	}

	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

// isHeadOfChain names llbuild rules that fire on every build regardless of
// source edits. Keeping them off the root-cause list stops them from
// drowning out real triggers. Extend this list when a new per-build-fired
// rule is identified on real artifacts.
//
// Currently caught: ClangStatCache (header stat cache refreshed every
// build), llbuild's stale-file-removal cleanup, and the "build headers"
// pre-compute aggregates.
func isHeadOfChain(key string) bool {
	if key == "" {
		return false
	}

	lower := strings.ToLower(key)

	switch {
	case strings.Contains(lower, "clangstatcache"),
		strings.Contains(lower, "clang-stat-cache"),
		strings.Contains(lower, "stale-file-removal"),
		strings.Contains(lower, "build-headers-stale-file-removal"):
		return true
	}

	return false
}

// isGateKey matches llbuild phony aggregation rules — the `Gate <scope>`
// commands that stitch together a target's phases without doing any work
// themselves. Also catches llbuild's `-immediate` suffixed variants.
func isGateKey(key string) bool {
	if key == "" {
		return false
	}

	// Strip the `C` rule-kind prefix llbuild adds in build.db so we match
	// the manifest-level token.
	stripped := key
	if isCommandKey(stripped) {
		stripped = stripped[1:]
	}

	// Priority prefix `P<N>:…:` — find the last `:` and look at the tail.
	// Simpler: tokenize by the trailing portion. Gate keys always embed
	// "Gate " or end in "-immediate".
	switch {
	case strings.Contains(stripped, ":Gate "),
		strings.HasPrefix(stripped, "Gate "),
		strings.HasSuffix(stripped, "-immediate"):
		return true
	}

	return false
}

// lookupRule walks the three key-shape attempts described in Classify's
// docstring. The build.db wraps Command manifest keys with a `C` prefix and
// Node (file) keys with `N` — tasks always map to Command rules, so we try the
// `C`-prefixed form first before falling back to raw keys for test fixtures
// and activity-log-only rows.
//
// Returns the matched map key alongside the rule so attribution can reuse it
// to look up Snapshot.Deps without re-running the same prefix dance.
func lookupRule(rules map[string]RuleResult, task *Task) (RuleResult, string, bool) {
	if task.ManifestKey != "" {
		if rule, ok := rules["C"+task.ManifestKey]; ok {
			return rule, "C" + task.ManifestKey, true
		}
		if rule, ok := rules[task.ManifestKey]; ok {
			return rule, task.ManifestKey, true
		}
	}

	if task.Key != "" {
		if rule, ok := rules["C"+task.Key]; ok {
			return rule, "C" + task.Key, true
		}
		if rule, ok := rules[task.Key]; ok {
			return rule, task.Key, true
		}
	}

	return RuleResult{}, "", false
}

// reasonForRule folds the iteration comparison into the six buckets. The
// `ran` flag splits the equal-iteration case: a matching iteration means
// llbuild re-evaluated the rule, but only an activity-log section proves
// the command actually executed — rules re-checked but not executed land
// in ReasonReEvaluatedCached so the operator can tell the two apart.
//
// The snap.Iteration == 0 fallback kicks in when the info row was empty
// (test fixtures with no info row, or an older schema that doesn't surface
// iteration) — in that mode the activity-log Ran flag is the only signal
// we have, and unknown-iteration rows read as Unclassified so the operator
// sees the ambiguity.
func reasonForRule(rule RuleResult, iteration int64, ran bool) ExecutionReason {
	if iteration == 0 {
		if ran {
			return ReasonRanThisBuild
		}

		return ReasonSkippedUpToDate
	}

	switch {
	case rule.Iteration == iteration && ran:
		return ReasonRanThisBuild
	case rule.Iteration == iteration && !ran:
		return ReasonReEvaluatedCached
	case rule.Iteration > 0 && rule.Iteration < iteration:
		return ReasonSkippedUpToDate
	default:
		return ReasonUnclassified
	}
}
