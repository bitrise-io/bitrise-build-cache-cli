package buildanalyzer

import (
	"encoding/hex"
)

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

// Classify walks every Task on report and sets ExecutionReason + BuiltAt +
// Signature from the Snapshot. Lookup is three-tiered, mirroring the match
// order the manifest join uses:
//
//  1. snap.Rules["C" + task.ManifestKey]  — the llbuild Command rule-kind
//     prefix the build.db wraps around manifest keys.
//  2. snap.Rules[task.ManifestKey]        — in case the prefix was not applied
//     (tests, older schema).
//  3. snap.Rules[task.Key]                — activity-log-only fallback.
//
// Classification then compares the rule's iteration counter (stored in
// RuleResult.BuiltAt via iterationToTime) against snap.Iteration:
//   - equal iteration → ReasonRanThisBuild
//   - lower iteration → ReasonSkippedUpToDate
//   - no row          → ReasonCold
//   - higher iteration → ReasonUnclassified (shouldn't happen, treated
//     defensively)
//
// When snap.Iteration is zero (e.g. the info row was empty) we fall back to
// activity-log Ran as the Rand/Skipped tiebreaker so warm-build output stays
// usable; the cold bucket still trips cleanly on missing rows.
func Classify(report *Report, snap Snapshot) {
	if report == nil {
		return
	}

	for i := range report.Tasks {
		task := &report.Tasks[i]

		rule, found := lookupRule(snap.Rules, task)
		if !found {
			task.ExecutionReason = ReasonCold

			continue
		}

		task.BuiltAt = rule.BuiltAt
		if len(rule.Signature) > 0 {
			task.Signature = hex.EncodeToString(rule.Signature)
		}

		task.ExecutionReason = reasonForRule(rule, snap.Iteration, task.Ran)
	}
}

// lookupRule walks the three key-shape attempts described in Classify's
// docstring. The build.db wraps Command manifest keys with a `C` prefix and
// Node (file) keys with `N` — tasks always map to Command rules, so we try the
// `C`-prefixed form first before falling back to raw keys for test fixtures
// and activity-log-only rows.
func lookupRule(rules map[string]RuleResult, task *Task) (RuleResult, bool) {
	if task.ManifestKey != "" {
		if rule, ok := rules["C"+task.ManifestKey]; ok {
			return rule, true
		}
		if rule, ok := rules[task.ManifestKey]; ok {
			return rule, true
		}
	}

	if task.Key != "" {
		if rule, ok := rules["C"+task.Key]; ok {
			return rule, true
		}
		if rule, ok := rules[task.Key]; ok {
			return rule, true
		}
	}

	return RuleResult{}, false
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

	ruleIteration := rule.BuiltAt.Unix()
	if rule.BuiltAt.IsZero() {
		ruleIteration = 0
	}

	switch {
	case ruleIteration == iteration:
		return ReasonRanThisBuild
	case ruleIteration > 0 && ruleIteration < iteration:
		return ReasonSkippedUpToDate
	default:
		return ReasonUnclassified
	}
}
