package buildanalyzer

import "time"

// PickSnapshotForLog picks the best snapshot pair for classifying a build
// whose activity-log finished at logFinishedAt.
//
// Semantics: snapshots are captured AFTER a build completes to freeze the
// state that build left behind. For a log that finished at T:
//
//   - current  = earliest snapshot with Timestamp > T (the post-build capture
//                that reflects this log's end state). When none exists, falls
//                back to the newest snapshot <= T (best effort — happens when
//                the user forgot to snapshot after the build).
//   - prior    = newest snapshot with Timestamp <= T (the previous build's
//                end state — baseline for signature-diff). Empty when no
//                pre-log snapshot exists.
//
// Iteration is intentionally NOT used as the primary tiebreaker: Xcode doesn't
// embed info.iteration in the xcactivitylog. Timestamp is the monotone signal
// we control.
func PickSnapshotForLog(storeRoot, buildHex string, logFinishedAt time.Time) (SnapshotMeta, SnapshotMeta, bool) {
	list, err := ListSnapshots(storeRoot, buildHex)
	if err != nil {
		return SnapshotMeta{}, SnapshotMeta{}, false
	}

	if len(list) == 0 {
		return SnapshotMeta{}, SnapshotMeta{}, false
	}

	var postLog, preLog []SnapshotMeta
	for _, meta := range list {
		if meta.Timestamp.After(logFinishedAt) {
			postLog = append(postLog, meta)
		} else {
			preLog = append(preLog, meta)
		}
	}

	var current, prior SnapshotMeta

	switch {
	case len(postLog) > 0:
		current = postLog[len(postLog)-1]
		if len(preLog) > 0 {
			prior = preLog[0]
		}
	case len(preLog) > 0:
		current = preLog[0]
		if len(preLog) > 1 {
			prior = preLog[1]
		}
	default:
		return SnapshotMeta{}, SnapshotMeta{}, false
	}

	return current, prior, true
}

// ReadSnapshotPair opens the current + prior snapshot paths and returns the
// Snapshot values with PriorRules populated on the current — the shape
// Classify expects.
//
// Prior path being empty means "no prior snapshot available"; the function
// returns the current Snapshot with PriorRules left nil so Classify runs the
// single-snapshot path. If reading prior fails, PriorRules stays nil and the
// error is returned non-fatally so the caller can log a warning and keep
// going — same soft-fail philosophy the single-snapshot path already uses.
func ReadSnapshotPair(currentPath, priorPath string) (Snapshot, error) {
	current, err := ReadBuildDB(currentPath)
	if err != nil {
		return Snapshot{}, err
	}

	if priorPath == "" {
		return current, nil
	}

	prior, err := ReadBuildDB(priorPath)
	if err != nil {
		// Soft-fail: return current without PriorRules. The caller can log
		// the error on stderr but the classifier still produces a useful
		// single-snapshot result.
		return current, err
	}

	current.PriorRules = prior.Rules

	return current, nil
}
