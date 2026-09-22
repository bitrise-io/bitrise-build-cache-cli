package enrichment

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"howett.net/plist"
)

// CI writes many entries per xcodebuild invocation and timestamps them with
// wall-clock skew, so the CI grouping window is wider than the local one.
//
//nolint:gochecknoglobals
var (
	LocalGroupTimeGap = 60 * time.Second
	CIGroupTimeGap    = 5 * time.Minute
)

type ManifestEntry struct {
	UUID       string
	ClassName  string
	Title      string
	Signature  string
	SchemeName string
	FileName   string
	Status     string
	Start      time.Time
	Stop       time.Time
}

type Command string

const (
	CommandBuild   Command = "build"
	CommandTest    Command = "test"
	CommandArchive Command = "archive"
	CommandUnknown Command = ""
)

func (m ManifestEntry) Command() Command {
	source := m.Signature
	if source == "" {
		source = m.Title
	}

	switch strings.ToLower(strings.SplitN(source, " ", 2)[0]) {
	case "build", "building", "cleaning", "clean":
		return CommandBuild
	case "test", "testing":
		return CommandTest
	case "archive", "archiving":
		return CommandArchive
	default:
		return CommandUnknown
	}
}

// Deliberately a blacklist: the full highLevelStatus set is undocumented, and
// requiring "S" mislabelled every warning-carrying build as failed.
const manifestStatusError = "E"

func (m ManifestEntry) Success() bool {
	return m.Status != manifestStatusError
}

// WalkManifests expands each glob against homeDir, loads every matched
// LogStoreManifest.plist, and invokes visit(manifestPath, entries) for each
// successfully parsed manifest. Glob or load failures are logged at debug and
// skipped — matching the pre-existing behavior of both Finder and Watcher.
func WalkManifests(homeDir string, globs []string, logger log.Logger, visit func(manifestPath string, entries []ManifestEntry)) {
	l := logOr(logger)

	for _, glob := range globs {
		matches, err := filepath.Glob(filepath.Join(homeDir, glob))
		if err != nil {
			l.Debugf("WalkManifests: glob %q failed: %s", glob, err)

			continue
		}

		for _, path := range matches {
			entries, err := LoadManifest(path)
			if err != nil {
				l.Debugf("WalkManifests: load %q failed: %s", path, err)

				continue
			}

			visit(path, entries)
		}
	}
}

// ManifestEntryGroup is a set of ManifestEntry rows from the same xcodebuild
// invocation. Grouping key is manifest-path + scheme + time-gap cluster; group
// aggregation lets the watcher emit one PUT per invocation instead of one per
// entry.
//
// Two known aggregation trade-offs:
//   - Cross-manifest fusion (Logs/Build/... + Logs/Test/...) is not attempted:
//     those live under different Logs/<subdir>/LogStoreManifest.plist paths
//     and are grouped independently.
//   - Command() diverges from the wrapper's ShortCommand() — the wrapper
//     renders "build [scheme / testPlan / config]" from argv, but the manifest
//     carries only the scheme name, so wrapper-less analytics runs show a
//     coarser command string.
//   - The aggregate span (Start = min, Stop = max) is wide by design: a burst
//     entry plus a late entry within TimeGap collapses into one group whose
//     span covers both. Correlation uses that span, so overlap-based matching
//     can pick a pending record that only overlaps part of the window.
type ManifestEntryGroup struct {
	Entries []ManifestEntry
}

func (g ManifestEntryGroup) UUIDs() []string {
	out := make([]string, 0, len(g.Entries))
	for _, e := range g.Entries {
		out = append(out, e.UUID)
	}

	return out
}

func (g ManifestEntryGroup) SchemeName() string {
	if len(g.Entries) == 0 {
		return ""
	}

	return g.Entries[0].SchemeName
}

func (g ManifestEntryGroup) Start() time.Time {
	var earliest time.Time

	for _, e := range g.Entries {
		if e.Start.IsZero() {
			continue
		}

		if earliest.IsZero() || e.Start.Before(earliest) {
			earliest = e.Start
		}
	}

	return earliest
}

func (g ManifestEntryGroup) Stop() time.Time {
	var latest time.Time

	for _, e := range g.Entries {
		if e.Stop.IsZero() {
			continue
		}

		if latest.IsZero() || e.Stop.After(latest) {
			latest = e.Stop
		}
	}

	return latest
}

func (g ManifestEntryGroup) Duration() time.Duration {
	start := g.Start()
	stop := g.Stop()

	if start.IsZero() || stop.IsZero() {
		return 0
	}

	return stop.Sub(start)
}

func (g ManifestEntryGroup) Success() bool {
	if len(g.Entries) == 0 {
		return false
	}

	for _, e := range g.Entries {
		if !e.Success() {
			return false
		}
	}

	return true
}

// Primary picks the outermost entry by command rank (test > archive > build >
// unknown). Ties are broken by earliest Start.
func (g ManifestEntryGroup) Primary() ManifestEntry {
	if len(g.Entries) == 0 {
		return ManifestEntry{}
	}

	rank := func(c Command) int {
		switch c {
		case CommandTest:
			return 3
		case CommandArchive:
			return 2
		case CommandBuild:
			return 1
		case CommandUnknown:
			return 0
		default:
			return 0
		}
	}

	best := g.Entries[0]
	bestRank := rank(best.Command())

	for _, e := range g.Entries[1:] {
		r := rank(e.Command())
		if r > bestRank || (r == bestRank && e.Start.Before(best.Start)) {
			best = e
			bestRank = r
		}
	}

	return best
}

// Command returns empty when the primary is unknown so the caller skips PUT.
func (g ManifestEntryGroup) Command() string {
	p := g.Primary()
	if p.Command() == CommandUnknown {
		return ""
	}

	if p.SchemeName == "" {
		return string(p.Command())
	}

	return string(p.Command()) + " " + p.SchemeName
}

func (g ManifestEntryGroup) FullCommand() string {
	return g.Primary().Signature
}

func GroupManifestEntries(entries []ManifestEntry, timeGap time.Duration) []ManifestEntryGroup {
	if len(entries) == 0 {
		return nil
	}

	byScheme := make(map[string][]ManifestEntry)
	schemeOrder := make([]string, 0)

	for _, e := range entries {
		if _, seen := byScheme[e.SchemeName]; !seen {
			schemeOrder = append(schemeOrder, e.SchemeName)
		}

		byScheme[e.SchemeName] = append(byScheme[e.SchemeName], e)
	}

	out := make([]ManifestEntryGroup, 0, len(schemeOrder))

	for _, scheme := range schemeOrder {
		bucket := byScheme[scheme]
		sort.SliceStable(bucket, func(i, j int) bool {
			return bucket[i].Start.Before(bucket[j].Start)
		})

		current := ManifestEntryGroup{Entries: []ManifestEntry{bucket[0]}}
		currentStop := bucket[0].Stop

		for _, e := range bucket[1:] {
			gap := e.Start.Sub(currentStop)
			if gap > timeGap {
				out = append(out, current)
				current = ManifestEntryGroup{Entries: []ManifestEntry{e}}
				currentStop = e.Stop

				continue
			}

			current.Entries = append(current.Entries, e)
			if e.Stop.After(currentStop) {
				currentStop = e.Stop
			}
		}

		out = append(out, current)
	}

	return out
}

func LoadManifestGrouped(path string, timeGap time.Duration) ([]ManifestEntryGroup, error) {
	entries, err := LoadManifest(path)
	if err != nil {
		return nil, err
	}

	return GroupManifestEntries(entries, timeGap), nil
}

func LoadManifest(path string) ([]ManifestEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer f.Close()

	var raw manifestFile
	if err := plist.NewDecoder(f).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}

	out := make([]ManifestEntry, 0, len(raw.Logs))

	for uuid, entry := range raw.Logs {
		status := entry.HighLevelStatus
		if status == "" {
			status = entry.PrimaryObservable.HighLevelStatus
		}

		out = append(out, ManifestEntry{
			UUID:       uuid,
			ClassName:  entry.ClassName,
			Title:      entry.Title,
			Signature:  entry.Signature,
			SchemeName: entry.SchemeName,
			FileName:   entry.FileName,
			Status:     status,
			Start:      cfAbsoluteToTime(entry.TimeStarted),
			Stop:       cfAbsoluteToTime(entry.TimeStopped),
		})
	}

	return out, nil
}

// CFAbsoluteTime zero = 2001-01-01 00:00:00 UTC.
const cfAbsoluteEpoch int64 = 978307200

func cfAbsoluteToTime(cfAbs float64) time.Time {
	if cfAbs == 0 {
		return time.Time{}
	}

	sec := int64(cfAbs)
	nsec := int64((cfAbs - float64(sec)) * 1e9)

	return time.Unix(sec+cfAbsoluteEpoch, nsec).UTC()
}

type manifestFile struct {
	Logs map[string]manifestLog `plist:"logs"`
}

type manifestLog struct {
	ClassName         string             `plist:"className"`
	Title             string             `plist:"title"`
	Signature         string             `plist:"signature"`
	SchemeName        string             `plist:"schemeIdentifier-schemeName"`
	FileName          string             `plist:"fileName"`
	HighLevelStatus   string             `plist:"highLevelStatus"`
	PrimaryObservable manifestObservable `plist:"primaryObservable"`
	TimeStarted       float64            `plist:"timeStartedRecording"`
	TimeStopped       float64            `plist:"timeStoppedRecording"`
}

type manifestObservable struct {
	HighLevelStatus string `plist:"highLevelStatus"`
}
