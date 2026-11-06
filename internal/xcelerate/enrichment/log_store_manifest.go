package enrichment

import (
	"fmt"
	"os"
	"sort"
	"time"

	"howett.net/plist"
)

// BuildBoundary is a single Build-domain entry from
// `<DerivedData>/Logs/Build/LogStoreManifest.plist`. It represents one master
// user action Xcode's IDE Report Navigator renders (e.g. "Build IceCubesApp").
//
// For a per-⌘B master, Start/Stop span the recording window of the main
// xcactivitylog. Sub-activities (dark-build xcbuilddata, indexer xcbuilddata,
// orphan proxy sidecars) landing at time T within [Start, Stop+grace] attribute
// to this boundary — the PR596 S7 algorithm.
//
// Scope: Build-domain only. The plist also carries Package / Update Signing /
// Localization entries that fire on Xcode launch without a user ⌘B; those are
// independent BE rows and live in sibling plists under Package/Signing/
// Localization subdirectories — not consumed by this reader.
type BuildBoundary struct {
	UniqueIdentifier string
	Signature        string
	SchemeName       string
	Status           string
	Start            time.Time
	Stop             time.Time
}

// ReadBuildBoundaries parses the plist at path and returns Build-domain
// entries sorted by Start ascending. Empty plist (no logs yet) returns
// (nil, nil); parse failure propagates err. Missing file returns the underlying
// os error so callers can distinguish "no DD / no builds yet" (ENOENT) from a
// real decode failure.
//
// The plist's logs map includes classes that map to Build, Package, Signing
// and Localization domains. This reader keeps only Build — identified by the
// highest-reliability className values Xcode emits:
//   - IDEActivityLogSection — the IDE's per-⌘B master entry
//   - IDECommandLineBuildLog — xcodebuild CLI master entry (also used for
//     Clean when the user clicks Clean in the IDE)
//
// Package/Signing/Localization entries carry different className values
// (IDEActivityLogPackageLoadLog, IDEActivityLogProvisioningSection, ...) that
// are not consumed here.
func ReadBuildBoundaries(path string) ([]BuildBoundary, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open build manifest: %w", err)
	}
	defer f.Close()

	var raw buildManifestFile
	if err := plist.NewDecoder(f).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode build manifest: %w", err)
	}

	out := make([]BuildBoundary, 0, len(raw.Logs))

	for uuid, entry := range raw.Logs {
		if !isBuildDomain(entry.ClassName, entry.DomainType) {
			continue
		}

		status := entry.HighLevelStatus
		if status == "" {
			status = entry.PrimaryObservable.HighLevelStatus
		}

		out = append(out, BuildBoundary{
			UniqueIdentifier: uuid,
			Signature:        entry.Signature,
			SchemeName:       entry.SchemeName,
			Status:           status,
			Start:            cfAbsoluteToTime(entry.TimeStarted),
			Stop:             cfAbsoluteToTime(entry.TimeStopped),
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Start.Before(out[j].Start)
	})

	return out, nil
}

const buildDomainType = "Xcode.IDEActivityLogDomainType.BuildLog"

// isBuildDomain filters the plist's logs map down to Build-domain entries.
// domainType is the authoritative source when present
// ("Xcode.IDEActivityLogDomainType.BuildLog"); className is the legacy
// fallback for older Xcode versions whose plist omits domainType.
//
// Returns true for IDE and CLI build logs, false for Package / Signing /
// Localization entries that fire passively on Xcode launch.
func isBuildDomain(className, domainType string) bool {
	if domainType != "" {
		return domainType == buildDomainType
	}

	switch className {
	case "IDEActivityLogSection", "IDECommandLineBuildLog":
		return true
	default:
		return false
	}
}

type buildManifestFile struct {
	Logs map[string]buildManifestLog `plist:"logs"`
}

type buildManifestLog struct {
	ClassName         string                  `plist:"className"`
	DomainType        string                  `plist:"domainType"`
	Signature         string                  `plist:"signature"`
	SchemeName        string                  `plist:"schemeIdentifier-schemeName"`
	HighLevelStatus   string                  `plist:"highLevelStatus"`
	PrimaryObservable buildManifestObservable `plist:"primaryObservable"`
	TimeStarted       float64                 `plist:"timeStartedRecording"`
	TimeStopped       float64                 `plist:"timeStoppedRecording"`
}

type buildManifestObservable struct {
	HighLevelStatus string `plist:"highLevelStatus"`
}

// EnclosingBuildBoundary returns the Build boundary whose Start ≤ at ≤ Stop+grace.
// When multiple candidates match (overlapping cancelled+restart edge cases),
// the one with the latest Start wins — the "most recent preceding Build" rule
// from the PR596 S7 attribution algorithm. Returns (BuildBoundary{}, false)
// when no boundary encloses at.
//
// Input boundaries are expected sorted by Start ascending (ReadBuildBoundaries
// output); callers that assemble boundaries elsewhere are responsible for
// ordering.
func EnclosingBuildBoundary(boundaries []BuildBoundary, at time.Time, grace time.Duration) (BuildBoundary, bool) {
	if at.IsZero() {
		return BuildBoundary{}, false
	}

	var (
		best  BuildBoundary
		found bool
	)

	for _, b := range boundaries {
		if b.Start.IsZero() || b.Stop.IsZero() {
			continue
		}
		if at.Before(b.Start) {
			continue
		}
		if at.After(b.Stop.Add(grace)) {
			continue
		}

		if !found || b.Start.After(best.Start) {
			best = b
			found = true
		}
	}

	return best, found
}

// DefaultAttributionGrace is the window past a Build boundary's Stop during
// which a late-arriving sub-activity (dark build fires ~35s after master start
// but can trail master stop by a few seconds; sidecars flush a few ticks
// after proxy close) still attributes to that boundary. Chosen at 5s per
// PR596 S7 architect design.
const DefaultAttributionGrace = 5 * time.Second
