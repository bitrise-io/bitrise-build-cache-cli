package versioncheck

type BumpKind int

const (
	NoChange BumpKind = iota
	FirstRun
	Bump
)

type DriftResult struct {
	Kind            BumpKind
	PreviousVersion string
	CurrentVersion  string
}

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
