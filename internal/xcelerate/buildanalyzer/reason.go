package buildanalyzer

import (
	"encoding/hex"
	"sort"
)

// changeCauseKeysLimit caps Task.ChangeCauseKeys entries so pretty / JSON
// output stays scannable; a sentinel "…" takes the final slot when truncated.
const changeCauseKeysLimit = 10

// ExecutionReason is the v2 bucket a task falls into once joined against the
// build.db Snapshot. The four values deliberately omit two interesting signals
// that would need a second snapshot or a dependency-graph walk — see the NON
// goals block in docs/xcode-build-analyzer.md:
//   - signature-mismatch vs new-task (needs the prior build.db).
//   - upstream-dependency attribution (needs rule_dependencies).
type ExecutionReason string

const (
	// ReasonCold — no rule_results row for this task in the current build.db.
	// Either the task is brand new or the DB has been wiped (clean build).
	ReasonCold ExecutionReason = "cold"
	// ReasonRanThisBuild — the task's rule row carries the current build's
	// iteration counter, i.e. llbuild re-evaluated it this build. Confirmed
	// by activity-log presence on the ran path.
	ReasonRanThisBuild ExecutionReason = "ran-this-build"
	// ReasonSkippedUpToDate — the task's rule row exists but carries an older
	// iteration, meaning llbuild reused the prior result. These are the
	// "incremental skipped" tasks that make warm builds fast.
	ReasonSkippedUpToDate ExecutionReason = "skipped-up-to-date"
	// ReasonUnclassified — defensive fall-through when neither the iteration
	// comparison nor the time windowing lands cleanly. A non-zero count on a
	// real build points at a matching-key gap to chase.
	ReasonUnclassified ExecutionReason = "unclassified"
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
//   - equal iteration → ReasonRanThisBuild
//   - lower iteration → ReasonSkippedUpToDate
//   - no row          → ReasonCold
//   - higher iteration → ReasonUnclassified (shouldn't happen, treated
//     defensively)
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
	}

	report.RootCauseKeys = collectRootCauseKeys(report.Tasks)
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
	task.RootCause = task.UpstreamRanCount == 0

	task.ChangeCauseKeys = immediateChangeCauses(snap, matchedKey)
}

// immediateChangeCauses walks the one-hop upstream Commands (no transitive
// walk) and keeps only those that ran this build. Truncated at
// changeCauseKeysLimit with a `…` sentinel. DirectCommandDeps dedupes a
// Command reached through multiple Node intermediates already — this filter
// only prunes skipped-upstream Commands.
func immediateChangeCauses(snap Snapshot, matchedKey string) []string {
	direct := DirectCommandDeps(snap, matchedKey)
	if len(direct) == 0 {
		return nil
	}

	out := make([]string, 0, len(direct))

	for _, cmd := range direct {
		rule, ok := snap.Rules[cmd]
		if !ok {
			continue
		}

		if ruleRanThisBuild(rule, snap.Iteration) {
			out = append(out, cmd)
		}
	}

	if len(out) == 0 {
		return nil
	}

	if len(out) > changeCauseKeysLimit {
		truncated := make([]string, 0, changeCauseKeysLimit+1)
		truncated = append(truncated, out[:changeCauseKeysLimit]...)
		truncated = append(truncated, "…")

		return truncated
	}

	return out
}

// collectRootCauseKeys scans the classified task list and returns the sorted
// unique set of Task.Key values where RootCause is set. Sorting keeps JSON
// diffs stable between runs on the same DB.
func collectRootCauseKeys(tasks []Task) []string {
	if len(tasks) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, 16)
	for _, task := range tasks {
		if !task.RootCause {
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

// reasonForRule folds the iteration comparison into the four buckets. The
// `ran` fallback kicks in when snap.Iteration is zero (test fixtures with no
// info row, or an older schema that doesn't surface iteration) — in that mode
// the activity-log Ran flag is the only signal we have, and unknown-iteration
// rows read as Unclassified so the operator sees the ambiguity.
func reasonForRule(rule RuleResult, iteration int64, ran bool) ExecutionReason {
	if iteration == 0 {
		if ran {
			return ReasonRanThisBuild
		}

		return ReasonSkippedUpToDate
	}

	switch {
	case rule.Iteration == iteration:
		return ReasonRanThisBuild
	case rule.Iteration > 0 && rule.Iteration < iteration:
		return ReasonSkippedUpToDate
	default:
		return ReasonUnclassified
	}
}
