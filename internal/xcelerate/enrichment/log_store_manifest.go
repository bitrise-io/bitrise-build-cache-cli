package enrichment

import (
	"fmt"
	"os"
	"sort"
	"time"

	"howett.net/plist"
)

// Boundary is a single Build-domain entry from
// `<DerivedData>/Logs/Build/LogStoreManifest.plist`. It represents one
// recording window Xcode's IDE Report Navigator surfaces for a ⌘B master
// build. Non-Build domain entries (Package / Signing / Localization) are no
// longer surfaced — the single-ID payload store owns cross-signal attribution.
type Boundary struct {
	UniqueIdentifier string
	Signature        string
	SchemeName       string
	Status           string
	Start            time.Time
	Stop             time.Time
}

// ReadBoundaries parses the plist at path and returns Build-domain entries
// sorted by Start ascending. Empty plist (no logs yet) returns (nil, nil);
// parse failure propagates err. Missing file returns the underlying os error
// so callers can distinguish "no DD / no builds yet" (ENOENT) from a real
// decode failure.
//
// Build-domain detection uses the authoritative domainType field when
// present, with a className fallback for older Xcode versions:
//   - domainType: Xcode.IDEActivityLogDomainType.BuildLog
//   - className:  IDEActivityLogSection / IDECommandLineBuildLog
func ReadBoundaries(path string) ([]Boundary, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open build manifest: %w", err)
	}
	defer f.Close()

	var raw buildManifestFile
	if err := plist.NewDecoder(f).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode build manifest: %w", err)
	}

	out := make([]Boundary, 0, len(raw.Logs))

	for uuid, entry := range raw.Logs {
		if !isBuildDomain(entry.ClassName, entry.DomainType) {
			continue
		}

		status := entry.HighLevelStatus
		if status == "" {
			status = entry.PrimaryObservable.HighLevelStatus
		}

		out = append(out, Boundary{
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

// EnclosingBoundary returns the boundary whose Start ≤ at ≤ Stop+grace. When
// multiple candidates match (overlapping cancelled+restart edge cases), the
// one with the latest Start wins — the "most recent preceding" rule from the
// PR596 S7 attribution algorithm. Returns (Boundary{}, false) when no
// boundary encloses at.
//
// Input boundaries are expected sorted by Start ascending (ReadBoundaries
// output); callers that assemble boundaries elsewhere are responsible for
// ordering.
func EnclosingBoundary(boundaries []Boundary, at time.Time, grace time.Duration) (Boundary, bool) {
	if at.IsZero() {
		return Boundary{}, false
	}

	var (
		best  Boundary
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

// DefaultAttributionGrace is the window past a boundary's Stop during which a
// late-arriving sub-activity (dark build fires ~35s after master start but
// can trail master stop by a few seconds; sidecars flush a few ticks after
// proxy close) still attributes to that boundary. Chosen at 5s per PR596 S7
// architect design.
const DefaultAttributionGrace = 5 * time.Second
