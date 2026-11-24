package buildanalyzer

import "time"

// PickSnapshotForLog picks the best snapshot for classifying a build whose
// activity-log finished at logFinishedAt. "Best" means the newest snapshot
// whose Timestamp is <= logFinishedAt — a snapshot taken AFTER the build
// finished is useless (it carries the post-build iteration), so the cutoff is
// strictly <=.
//
// Returns (meta, true) and also the prior snapshot (iter N-1 by timestamp) for
// the signature-diff walk. When no snapshot is <= logFinishedAt (store empty,
// or every snapshot was taken after the log's finish-time), returns
// (SnapshotMeta{}, false) and the empty prior. When exactly one snapshot
// matches the primary cutoff, prior is zero (no diff signal available).
//
// Iteration is intentionally NOT used as the primary tiebreaker: Xcode doesn't
// embed info.iteration in the xcactivitylog, so the caller can't ask "which
// snapshot's iteration matches this log". Timestamp is the best monotone
// signal we control. Among snapshots with identical timestamps (synthetic
// fixtures only), higher iteration wins — ListSnapshots already sorts that
// way.
func PickSnapshotForLog(storeRoot, buildHex string, logFinishedAt time.Time) (SnapshotMeta, SnapshotMeta, bool) {
	list, err := ListSnapshots(storeRoot, buildHex)
	if err != nil {
		// A read failure here is a soft signal — classification skips the
		// prior-snapshot path and Classify falls back to the single-snapshot
		// behavior. The CLI layer surfaces the underlying error on its own
		// channel before this gets called.
		return SnapshotMeta{}, SnapshotMeta{}, false
	}

	if len(list) == 0 {
		return SnapshotMeta{}, SnapshotMeta{}, false
	}

	// ListSnapshots returns iteration-desc. Walk in that order and take the
	// first whose Timestamp sits at or before the cutoff — that's the "most
	// recent snapshot that could possibly describe this log".
	for i, meta := range list {
		if !meta.Timestamp.After(logFinishedAt) {
			var prior SnapshotMeta
			if i+1 < len(list) {
				prior = list[i+1]
			}

			return meta, prior, true
		}
	}

	return SnapshotMeta{}, SnapshotMeta{}, false
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
