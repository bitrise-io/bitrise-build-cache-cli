package wrapperless

import "time"

type PrimaryAction string

const (
	PrimaryActionBuild   PrimaryAction = "build"
	PrimaryActionArchive PrimaryAction = "archive"
	PrimaryActionTest    PrimaryAction = "test"
	PrimaryActionAnalyze PrimaryAction = "analyze"
)

type Start struct {
	At              time.Time
	DerivedDataPath string
	BuildRequestHex string
	Action          PrimaryAction
}

type End struct {
	At               time.Time
	Scheme           string
	Project          string
	Workspace        string
	Configuration    string
	Action           PrimaryAction
	Success          bool
	UniqueIdentifier string
	Duration         time.Duration
	// XCActivityLogPath is the sealed sibling log at
	// <ddRoot>/Logs/Build/<uniqueIdentifier>.xcactivitylog. Enriched-emit path
	// reads CompilationCacheMetrics from here; empty when the manifest
	// predates that layout.
	XCActivityLogPath string
}

type Quit struct {
	At          time.Time
	PID         int
	StartTimeMS int64
	AncestryIDE bool
}
