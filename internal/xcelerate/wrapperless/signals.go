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
	Success          bool
	UniqueIdentifier string
	Duration         time.Duration
}

type Quit struct {
	At          time.Time
	PID         int
	AncestryIDE bool
}
