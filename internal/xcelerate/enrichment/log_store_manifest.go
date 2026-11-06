package enrichment

import (
	"fmt"
	"os"
	"sort"
	"time"

	"howett.net/plist"
)

// Domain classifies a LogStoreManifest.plist entry by the user action that
// recorded it. Attribution bridging treats each domain differently:
// DomainBuild entries pair with the enricher's manifest emissions (dark build
// + indexer traffic attribute back to a ⌘B); the other three fire on Xcode
// launch without a user ⌘B and only surface when an orphan sidecar's window
// lands inside their recording boundary.
type Domain string

const (
	DomainBuild        Domain = "build"
	DomainPackage      Domain = "package"
	DomainSigning      Domain = "signing"
	DomainLocalization Domain = "localization"
)

// Boundary is a single attribution-eligible entry from
// `<DerivedData>/Logs/<subdir>/LogStoreManifest.plist`. It represents one
// recording window Xcode's IDE Report Navigator surfaces: either a Build
// master (per-⌘B) or an SPM/signing/localization sub-activity that fires on
// Xcode launch.
//
// Sub-activities landing at time T within [Start, Stop+grace] attribute to
// this boundary — the PR596 S7+S8 algorithm. Domain lets the watcher's bridge
// path render the right label ("Resolve Packages", "Update Signing", "Sync
// Localizations") for pre-build CAS traffic that would otherwise fall through
// to a bare "build" row.
type Boundary struct {
	UniqueIdentifier string
	Domain           Domain
	Signature        string
	SchemeName       string
	Status           string
	Start            time.Time
	Stop             time.Time
}

// ReadBoundaries parses the plist at path and returns entries across all
// attribution-eligible domains (Build, Package, Signing, Localization) sorted
// by Start ascending. Empty plist (no logs yet) returns (nil, nil); parse
// failure propagates err. Missing file returns the underlying os error so
// callers can distinguish "no DD / no builds yet" (ENOENT) from a real decode
// failure.
//
// Each domain is identified by the authoritative domainType field when
// present, with a className fallback for older Xcode versions whose plist
// omits domainType:
//   - Build:        IDEActivityLogSection / IDECommandLineBuildLog
//   - Package:      IDEActivityLogPackageLoadLog / IDEActivityLogPackageResolutionSection
//   - Signing:      IDEActivityLogProvisioningSection / IDEActivityLogSigningSection
//   - Localization: IDEActivityLogLocalizationSection / IDEActivityLogExportLocalizationsSection
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
		domain, ok := classifyDomain(entry.ClassName, entry.DomainType)
		if !ok {
			continue
		}

		status := entry.HighLevelStatus
		if status == "" {
			status = entry.PrimaryObservable.HighLevelStatus
		}

		out = append(out, Boundary{
			UniqueIdentifier: uuid,
			Domain:           domain,
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

const (
	buildDomainType        = "Xcode.IDEActivityLogDomainType.BuildLog"
	packageDomainType      = "Xcode.IDEActivityLogDomainType.PackageLog"
	signingDomainType      = "Xcode.IDEActivityLogDomainType.ProvisioningLog"
	localizationDomainType = "Xcode.IDEActivityLogDomainType.LocalizationLog"
)

// classifyDomain maps a plist entry to its attribution Domain. domainType is
// authoritative when present; className is the legacy fallback for older
// Xcode versions whose plist omits it. Returns ok=false for classNames /
// domainTypes outside the attribution-eligible set so callers skip the entry.
func classifyDomain(className, domainType string) (Domain, bool) {
	if domainType != "" {
		switch domainType {
		case buildDomainType:
			return DomainBuild, true
		case packageDomainType:
			return DomainPackage, true
		case signingDomainType:
			return DomainSigning, true
		case localizationDomainType:
			return DomainLocalization, true
		default:
			return "", false
		}
	}

	switch className {
	case "IDEActivityLogSection", "IDECommandLineBuildLog":
		return DomainBuild, true
	case "IDEActivityLogPackageLoadLog", "IDEActivityLogPackageResolutionSection":
		return DomainPackage, true
	case "IDEActivityLogProvisioningSection", "IDEActivityLogSigningSection":
		return DomainSigning, true
	case "IDEActivityLogLocalizationSection", "IDEActivityLogExportLocalizationsSection":
		return DomainLocalization, true
	default:
		return "", false
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
