package versioncheck

// BumpKind classifies the direction of the version delta between the
// previously-persisted version and the currently-running binary.
type BumpKind int

const (
	// NoChange — running binary version matches the persisted version.
	NoChange BumpKind = iota
	// FirstRun — no previous version persisted; this is the first time the
	// CLI wrote state. Not a bump per se, but D3 may still want to refresh
	// configs the first time.
	FirstRun
	// Bump — running binary version is different from the persisted one.
	// Could be a forward upgrade or (rarely) a downgrade after a rollback;
	// we treat both as "config-refresh worthy" because either direction can
	// change generated config shape.
	Bump
)

// DriftResult describes what changed between persisted state and the running
// binary. Returned by Detect for the caller to decide whether to trigger
// downstream refresh (D3) or emit a user-facing message.
type DriftResult struct {
	Kind            BumpKind
	PreviousVersion string
	CurrentVersion  string
}

// Detect compares the running binary's currentVersion against the persisted
// state. Pure function — no I/O — so callers can compose it with their own
// state lookup.
func Detect(state State, currentVersion string) DriftResult {
	switch state.LastVersion {
	case "":
		return DriftResult{Kind: FirstRun, CurrentVersion: currentVersion}
	case currentVersion:
		return DriftResult{Kind: NoChange, PreviousVersion: state.LastVersion, CurrentVersion: currentVersion}
	default:
		return DriftResult{Kind: Bump, PreviousVersion: state.LastVersion, CurrentVersion: currentVersion}
	}
}
