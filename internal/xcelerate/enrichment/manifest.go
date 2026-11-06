package enrichment

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"howett.net/plist"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcactivitylog"
)

// MetricsLookup resolves an entry's xcactivitylog metrics. The Watcher wires
// a per-tick cache so each log is gunzipped at most once per scan; nil
// disables the lookup (grouping falls back to one-group-per-entry;
// aggregateHitRate reads directly).
type MetricsLookup func(entry ManifestEntry) xcactivitylog.Metrics

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
	CommandClean   Command = "clean"
	CommandUnknown Command = ""
)

func (m ManifestEntry) Command() Command {
	source := m.Signature
	if source == "" {
		source = m.Title
	}

	switch strings.ToLower(strings.SplitN(source, " ", 2)[0]) {
	case "build", "building":
		return CommandBuild
	case "test", "testing":
		return CommandTest
	case "archive", "archiving":
		return CommandArchive
	case "clean", "cleaning":
		return CommandClean
	default:
		return CommandUnknown
	}
}

// Deliberately a blacklist: the full highLevelStatus set is undocumented, and
// requiring "S" mislabelled every warning-carrying build as failed.
const manifestStatusError = "E"

//nolint:gochecknoglobals
var (
	configurationFromSignature = regexp.MustCompile(`(?i)\bconfiguration\s+(\S+)`)
	projectFromSignature       = regexp.MustCompile(`(?i)\bproject\s+(\S+)`)
	workspaceFromSignature     = regexp.MustCompile(`(?i)\bworkspace\s+(\S+)`)
)

// Configuration parses the configuration name out of Xcode's Signature string.
func (m ManifestEntry) Configuration() string {
	if match := configurationFromSignature.FindStringSubmatch(m.Signature); len(match) == 2 {
		return match[1]
	}

	return ""
}

// Project parses the project name out of Xcode's Signature string.
func (m ManifestEntry) Project() string {
	if match := projectFromSignature.FindStringSubmatch(m.Signature); len(match) == 2 {
		return match[1]
	}

	return ""
}

// Workspace parses the workspace name out of Xcode's Signature string.
// Signatures from `xcodebuild -workspace Foo.xcworkspace -scheme Bar` land as
// "Building workspace Foo with scheme Bar" and omit any project/configuration
// token, so FullCommand needs the workspace to render an argv-shaped command.
func (m ManifestEntry) Workspace() string {
	if match := workspaceFromSignature.FindStringSubmatch(m.Signature); len(match) == 2 {
		return match[1]
	}

	return ""
}

func (m ManifestEntry) Success() bool {
	return m.Status != manifestStatusError
}

// ErrorMessage synthesizes a failure string — manifest has no richer payload.
func (m ManifestEntry) ErrorMessage() string {
	if m.Success() {
		return ""
	}

	return "xcodebuild failed (status=" + m.Status + ")"
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

// ManifestEntryGroup is a single ManifestEntry row from a single
// LogStoreManifest.plist — one group per entry, one BE row per user action
// Xcode's Report Navigator surfaces. Sub-activities (dark build, indexer)
// land as their own sidecars; the single-ID payload store owns cross-signal
// attribution under the proxy-derived InvocationID.
//
// Invariant (relied on by the enricher's sibling-log resolve): a group never
// mixes entries from different manifests.
//
// Known aggregation trade-off: Command() diverges from the wrapper's
// ShortCommand() — the wrapper renders "build [scheme / testPlan / config]"
// from argv, but the manifest carries only the scheme name, so wrapper-less
// analytics runs show a coarser command string.
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

// PrimarySpan is the time span of Primary() alone — the invocation anchor's
// own build-action window, not the wider cluster that includes post-build
// indexing / preview compiles. Used by the BE row payload so durationMs
// reflects the build, not the aggregate cluster.
func (g ManifestEntryGroup) PrimarySpan() time.Duration {
	p := g.Primary()
	if p.Start.IsZero() || p.Stop.IsZero() {
		return 0
	}

	return p.Stop.Sub(p.Start)
}

// ErrorMessage returns the first non-empty entry message so a mixed-status
// group surfaces failure signal while mirroring Success()'s AND semantics.
func (g ManifestEntryGroup) ErrorMessage() string {
	for _, e := range g.Entries {
		if msg := e.ErrorMessage(); msg != "" {
			return msg
		}
	}

	return ""
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

// commandRank orders Command values so the Primary picker ranks them:
// archive > build > test > clean > unknown.
//
// Build outranks Test because build-for-testing and test share a scheme and
// cluster into one group, and the test-without-building log never carries
// compile-cache events — reading hit rate off the test entry pins the group
// at 0 even when build-for-testing hit the cache.
func commandRank(c Command) int {
	switch c {
	case CommandArchive:
		return 3
	case CommandBuild:
		return 2
	case CommandTest:
		return 1
	case CommandClean:
		return 0
	case CommandUnknown:
		return -1
	default:
		return -1
	}
}

// Primary picks the outermost entry by command rank (archive > build > test >
// clean > unknown), then earliest Start, then lex-smallest UUID (deterministic
// across Go-map iteration order).
func (g ManifestEntryGroup) Primary() ManifestEntry {
	if len(g.Entries) == 0 {
		return ManifestEntry{}
	}

	best := g.Entries[0]
	bestRank := commandRank(best.Command())

	for _, e := range g.Entries[1:] {
		r := commandRank(e.Command())
		switch {
		case r > bestRank:
			best = e
			bestRank = r
		case r == bestRank && e.Start.Before(best.Start):
			best = e
		case r == bestRank && e.Start.Equal(best.Start) && e.UUID < best.UUID:
			best = e
		}
	}

	return best
}

// ManifestUUID returns Primary().UUID; always manifest-sourced now that
// synthetic pseudo-groups no longer mint UUIDs.
func (g ManifestEntryGroup) ManifestUUID() string {
	return g.Primary().UUID
}

// Command returns empty when the primary carries no known action so the caller
// skips PUT. Renders `<action> [<scheme> / <configuration>]` — Primary-only.
// Each independent user action (IDE Clean, IDE Build, CLI invocation) is its
// own group and lands as its own BE row.
func (g ManifestEntryGroup) Command() string {
	p := g.Primary()
	if p.Command() == CommandUnknown {
		return ""
	}

	parts := make([]string, 0, 2)
	if scheme := g.SchemeName(); scheme != "" {
		parts = append(parts, scheme)
	}

	if cfg := p.Configuration(); cfg != "" {
		parts = append(parts, cfg)
	}

	if len(parts) == 0 {
		return string(p.Command())
	}

	return string(p.Command()) + " [" + strings.Join(parts, " / ") + "]"
}

// FullCommand renders an argv-shaped xcodebuild invocation for the Primary
// action. -workspace / -project / -configuration are sourced via
// firstNonEmptyTokens so a split-companion entry (e.g. clean sharing the
// scheme group but carrying the project+configuration tokens Build omitted)
// still contributes its metadata. Falls back to Primary().Signature verbatim
// when Primary's command is Unknown and no ws/project/configuration could be
// parsed.
func (g ManifestEntryGroup) FullCommand() string {
	p := g.Primary()
	ws, proj, cfg := g.firstNonEmptyTokens()

	if p.Command() == CommandUnknown && ws == "" && proj == "" && cfg == "" {
		return p.Signature
	}

	parts := make([]string, 0, 5)
	parts = append(parts, "xcodebuild")

	if p.Command() != CommandUnknown {
		parts = append(parts, string(p.Command()))
	}

	switch {
	case ws != "":
		parts = append(parts, "-workspace", ws)
	case proj != "":
		parts = append(parts, "-project", proj)
	}

	if p.SchemeName != "" {
		parts = append(parts, "-scheme", p.SchemeName)
	}

	if cfg != "" {
		parts = append(parts, "-configuration", cfg)
	}

	return strings.Join(parts, " ")
}

// firstNonEmptyTokens walks entries in Start order and picks the first
// non-empty workspace, project, and configuration. Shared by FullCommand so a
// split-companion entry still contributes its metadata when Primary's own
// Signature omits a token.
func (g ManifestEntryGroup) firstNonEmptyTokens() (string, string, string) {
	ordered := append([]ManifestEntry(nil), g.Entries...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Start.Before(ordered[j].Start)
	})

	var ws, proj, cfg string

	for _, e := range ordered {
		if ws == "" {
			ws = e.Workspace()
		}

		if proj == "" {
			proj = e.Project()
		}

		if cfg == "" {
			cfg = e.Configuration()
		}

		if ws != "" && proj != "" && cfg != "" {
			break
		}
	}

	return ws, proj, cfg
}

// GroupOption configures GroupManifestEntries. Preserved as the extension
// seam even though the body is currently a no-op: callers (the Watcher) still
// pass WithMetricsLookup for hit-rate aggregation via Enricher.SetMetricsLookup,
// and keeping the parameter avoids churning every call site each time the
// grouping axis changes.
type GroupOption func(*groupOptions)

type groupOptions struct {
	metricsLookup MetricsLookup
}

// WithMetricsLookup wires the per-entry xcactivitylog metrics source. Not
// consulted at grouping time; the Enricher's hit-rate aggregation reads
// through the same seam (WithMetricsLookup → SetMetricsLookup) so call sites
// build the lookup once.
func WithMetricsLookup(fn MetricsLookup) GroupOption {
	return func(o *groupOptions) {
		o.metricsLookup = fn
	}
}

// GroupManifestEntries expects entries from a single manifest — see the
// ManifestEntryGroup invariant. Returns one group per entry, ordered by Start.
// Sub-activity attribution (dark builds, indexers) happens at the watcher's
// orphan-bridge path via plist-boundary windows, not here.
func GroupManifestEntries(entries []ManifestEntry, opts ...GroupOption) []ManifestEntryGroup {
	if len(entries) == 0 {
		return nil
	}

	var cfg groupOptions
	for _, opt := range opts {
		opt(&cfg)
	}
	_ = cfg // reserved for future grouping axes; see GroupOption.

	ordered := append([]ManifestEntry(nil), entries...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Start.Before(ordered[j].Start)
	})

	out := make([]ManifestEntryGroup, 0, len(ordered))
	for _, e := range ordered {
		out = append(out, ManifestEntryGroup{Entries: []ManifestEntry{e}})
	}

	return out
}

func LoadManifestGrouped(path string, opts ...GroupOption) ([]ManifestEntryGroup, error) {
	entries, err := LoadManifest(path)
	if err != nil {
		return nil, err
	}

	return GroupManifestEntries(entries, opts...), nil
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
