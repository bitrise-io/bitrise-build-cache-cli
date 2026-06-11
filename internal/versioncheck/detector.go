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
	// Bump — running binary version differs from the persisted one. The
	// classification is direction-agnostic on purpose: forward upgrade is
	// the common case, but a homebrew downgrade or an installer.sh pin to
	// an older tag changes the persisted-vs-running delta in the other
	// direction and the generated config shape may still differ between
	// versions either way. Treating both as "refresh-worthy" keeps the
	// downstream nudge (refresh.OnBump in D3) trustworthy without forcing
	// it to compare semver, which the CLI's `devel` and snapshot versions
	// can't be parsed as.
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
