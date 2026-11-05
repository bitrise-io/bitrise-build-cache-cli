//go:build unit

package enrichment

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
)

func writeFixtureManifest(t *testing.T, home string) {
	t.Helper()

	derived := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-abc/Logs/Build")
	require.NoError(t, os.MkdirAll(derived, 0o755))

	fixture, err := os.ReadFile("testdata/LogStoreManifest.plist")
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(derived, "LogStoreManifest.plist"), fixture, 0o644))
}

// fixtureNow pins the clock near the fixture manifest's own timestamps. The
// scan age gate drops entries whose Stop is older than HandledManifestMaxAge, so
// without this every test using the fixture starts failing once the committed
// timestamps age past that window.
var fixtureNow = time.Date(2025, 2, 27, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals

// pickUUID returns any UUID present in the fixture manifest — the test only cares
// about state transitions, not which specific entry.
func pickUUID(t *testing.T, home string) string {
	t.Helper()

	entries, err := LoadManifest(filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-abc/Logs/Build/LogStoreManifest.plist"))
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	return entries[0].UUID
}

func TestWatcher_scan_MatchedEntry_FiresImmediately(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)

	var calls int
	var uuids []string
	w := &Watcher{
		Now:     func() time.Time { return fixtureNow },
		HomeDir: home,
		Handle: func(_ string, g ManifestEntryGroup) {
			calls++
			uuids = append(uuids, g.UUIDs()...)
		},
		MatchProbe:            func(ManifestEntryGroup) bool { return true },
		MaxCorrelationRetries: 3,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)

	assert.Positive(t, calls)
	assert.Empty(t, w.retries)
	assert.Len(t, w.seen, len(uuids))
}

func TestWatcher_scan_UnmatchedEntry_HeldForRetries(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)
	uuid := pickUUID(t, home)

	var scans int
	var handled []string
	w := &Watcher{
		Now:     func() time.Time { return fixtureNow },
		HomeDir: home,
		Handle: func(_ string, g ManifestEntryGroup) {
			if groupContainsUUID(g, uuid) {
				handled = append(handled, uuid)
			}
		},
		MatchProbe: func(g ManifestEntryGroup) bool {
			if !groupContainsUUID(g, uuid) {
				return true
			}

			return scans >= 3
		},
		MaxCorrelationRetries: 3,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	scans = 1
	w.scan(false, false)
	assert.Empty(t, handled, "held on first scan while MatchProbe still false")

	scans = 2
	w.scan(false, false)
	assert.Empty(t, handled, "still held on second scan")

	scans = 3
	w.scan(false, false)
	assert.Equal(t, []string{uuid}, handled, "fires on the scan MatchProbe first returns true")
	assert.Contains(t, w.seen, uuid)
}

func groupContainsUUID(g ManifestEntryGroup, uuid string) bool {
	for _, u := range g.UUIDs() {
		if u == uuid {
			return true
		}
	}

	return false
}

func TestWatcher_scan_UnmatchedEntry_MintedAsOrphanAfterMaxRetries(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)
	uuid := pickUUID(t, home)

	var handled []string
	w := &Watcher{
		Now:     func() time.Time { return fixtureNow },
		HomeDir: home,
		Handle: func(_ string, g ManifestEntryGroup) {
			if groupContainsUUID(g, uuid) {
				handled = append(handled, uuid)
			}
		},
		MatchProbe: func(g ManifestEntryGroup) bool {
			return !groupContainsUUID(g, uuid)
		},
		MaxCorrelationRetries: 2,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	// MaxCorrelationRetries=2 gives 2 retry scans held after the fresh sight,
	// and orphan-fires on the next (4th total) scan.
	w.scan(false, false)
	assert.Empty(t, handled, "first sight: buckets the group for retry")
	assert.NotEmpty(t, w.retries, "retry bucket must contain the held group")
	assert.NotContains(t, w.seen, uuid)

	w.scan(false, false)
	assert.Empty(t, handled, "retry scan 1: decrements 2->1, still held")

	w.scan(false, false)
	assert.Empty(t, handled, "retry scan 2: decrements 1->0, still held")

	w.scan(false, false)
	assert.Equal(t, []string{uuid}, handled, "retries exhausted: mints as orphan")
	assert.Contains(t, w.seen, uuid)
}

func TestWatcher_scan_ZeroRetries_MintsImmediately(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)
	uuid := pickUUID(t, home)

	var handled []string
	w := &Watcher{
		Now:     func() time.Time { return fixtureNow },
		HomeDir: home,
		Handle: func(_ string, g ManifestEntryGroup) {
			if groupContainsUUID(g, uuid) {
				handled = append(handled, uuid)
			}
		},
		MatchProbe: func(ManifestEntryGroup) bool { return false },
		// MaxCorrelationRetries = 0 → opt out; fires immediately.
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)

	assert.Equal(t, []string{uuid}, handled)
	assert.Contains(t, w.seen, uuid)
}

func TestWatcher_scan_NilMatchProbe_FiresImmediately(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)
	uuid := pickUUID(t, home)

	var handled []string
	w := &Watcher{
		Now:     func() time.Time { return fixtureNow },
		HomeDir: home,
		Handle: func(_ string, g ManifestEntryGroup) {
			if groupContainsUUID(g, uuid) {
				handled = append(handled, uuid)
			}
		},
		MatchProbe:            nil,
		MaxCorrelationRetries: 6,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)

	assert.Equal(t, []string{uuid}, handled)
	assert.Contains(t, w.seen, uuid)
}

func TestWatcher_scan_SeedOnlyDoesNotPopulateRetries(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)
	uuid := pickUUID(t, home)

	var handled []string
	w := &Watcher{
		Now:     func() time.Time { return fixtureNow },
		HomeDir: home,
		Handle: func(_ string, g ManifestEntryGroup) {
			handled = append(handled, g.UUIDs()...)
		},
		MatchProbe:            func(ManifestEntryGroup) bool { return false },
		MaxCorrelationRetries: 6,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(true, false)

	assert.Empty(t, handled, "seed scan must not emit")
	assert.Contains(t, w.seen, uuid)
	assert.Empty(t, w.retries, "seed scan must not populate retries")
}

func TestWatcher_scan_PendingUUIDNotSkippedBySeenCheck(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)
	uuid := pickUUID(t, home)

	w := &Watcher{
		Now:                   func() time.Time { return fixtureNow },
		HomeDir:               home,
		Handle:                func(string, ManifestEntryGroup) {},
		MatchProbe:            func(ManifestEntryGroup) bool { return false },
		MaxCorrelationRetries: 6,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)

	assert.NotEmpty(t, w.retries, "held groups must land in retries after first unmatched scan")
	assert.NotContains(t, w.seen, uuid, "unmatched uuid must NOT be in seen (otherwise second scan is skipped)")
}

// writeInlineManifest writes a minimal manifest with a single entry named uuid at
// derivedRoot/Logs/<subdir>/LogStoreManifest.plist.
func writeInlineManifest(t *testing.T, derivedRoot, subdir, uuid string) {
	t.Helper()

	dir := filepath.Join(derivedRoot, "Logs", subdir)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>logs</key>
	<dict>
		<key>` + uuid + `</key>
		<dict>
			<key>fileName</key><string>` + uuid + `.xcactivitylog</string>
			<key>highLevelStatus</key><string>S</string>
			<key>signature</key><string>Build FreshScheme</string>
			<key>timeStartedRecording</key><real>762345900.0</real>
			<key>timeStoppedRecording</key><real>762345910.0</real>
		</dict>
	</dict>
</dict>
</plist>`

	require.NoError(t, os.WriteFile(filepath.Join(dir, "LogStoreManifest.plist"), []byte(plist), 0o644))
}

func TestWatcher_scan_MultipleGlobsBothObserved(t *testing.T) {
	home := t.TempDir()

	// Two DerivedData roots (default Library/... layout + wrapper-managed .bitrise/cache/xcode-dd/<sha>).
	defaultRoot := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-abc")
	managedRoot := filepath.Join(home, ".bitrise/cache/xcode-dd/deadbeef")

	writeInlineManifest(t, defaultRoot, "Build", "UUID-DEFAULT")
	writeInlineManifest(t, managedRoot, "Build", "UUID-MANAGED")

	var handled []string
	w := &Watcher{
		Now:     func() time.Time { return fixtureNow },
		HomeDir: home,
		Globs: []string{
			DefaultDerivedDataGlob,
			".bitrise/cache/xcode-dd/*/Logs/*/LogStoreManifest.plist",
		},
		Handle: func(_ string, g ManifestEntryGroup) {
			handled = append(handled, g.UUIDs()...)
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)

	assert.ElementsMatch(t, []string{"UUID-DEFAULT", "UUID-MANAGED"}, handled,
		"both glob roots must be observed on a single scan")
}

func TestWatcher_scan_EmptyGlobsFallsBackToDefault(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)
	uuid := pickUUID(t, home)

	var handled []string
	w := &Watcher{
		Now:     func() time.Time { return fixtureNow },
		HomeDir: home,
		Globs:   nil,
		Handle: func(_ string, g ManifestEntryGroup) {
			handled = append(handled, g.UUIDs()...)
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)

	assert.Contains(t, handled, uuid, "nil Globs must fall back to DefaultDerivedDataGlob")
}

func TestWatcher_scan_MatchesPackageSubdirManifest(t *testing.T) {
	home := t.TempDir()
	derivedRoot := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-pkg")
	writeInlineManifest(t, derivedRoot, "Package", "UUID-PACKAGE")

	var handled []string
	w := &Watcher{
		Now:     func() time.Time { return fixtureNow },
		HomeDir: home,
		Handle: func(_ string, g ManifestEntryGroup) {
			handled = append(handled, g.UUIDs()...)
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)

	assert.Equal(t, []string{"UUID-PACKAGE"}, handled,
		"Logs/Package/ manifest (Xcode 26 package builds) must match the default glob")
}

func TestWatcher_scan_HydratesSeenFromHandledStoreOnStartup(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)
	uuid := pickUUID(t, home)

	// Pre-populate the HandledStore with the manifest's UUID so Run's
	// startup Load treats it as already-seen and no emit fires.
	storePath := filepath.Join(t.TempDir(), "handled.ndjson")
	store := &HandledManifestStore{Path: storePath}
	require.NoError(t, store.Append(HandledManifest{UUID: uuid, HandledAt: time.Now()}))

	var handled []string
	w := &Watcher{
		Now:     func() time.Time { return fixtureNow },
		HomeDir: home,
		Handle: func(_ string, g ManifestEntryGroup) {
			handled = append(handled, g.UUIDs()...)
		},
		HandledStore: store,
		PollInterval: time.Hour, // never tick; only startup block runs
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel BEFORE Run so the ticker loop exits immediately after init

	w.Run(ctx)

	assert.Empty(t, handled, "startup must not emit a UUID already in HandledStore")
	assert.Contains(t, w.seen, uuid, "seen must be hydrated from HandledStore")
}

func TestWatcher_scan_AppendsHandledOnEmit(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)
	uuid := pickUUID(t, home)

	storePath := filepath.Join(t.TempDir(), "handled.ndjson")
	store := &HandledManifestStore{Path: storePath}

	w := &Watcher{
		HomeDir:      home,
		Handle:       func(string, ManifestEntryGroup) {},
		HandledStore: store,
		Now:          func() time.Time { return fixtureNow },
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)

	loaded, err := store.Load()
	require.NoError(t, err)

	var uuids []string
	for _, r := range loaded {
		uuids = append(uuids, r.UUID)
	}
	assert.Contains(t, uuids, uuid, "emitted UUID must be appended to HandledStore")
}

func writeMultiEntryManifest(t *testing.T, derivedRoot string, uuids ...string) {
	t.Helper()

	dir := filepath.Join(derivedRoot, "Logs", "Build")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>logs</key>
	<dict>
`)

	start := 762345900.0
	for i, uuid := range uuids {
		entryStart := start + float64(i)*10
		entryStop := entryStart + 5
		b.WriteString(`		<key>` + uuid + `</key>
		<dict>
			<key>fileName</key><string>` + uuid + `.xcactivitylog</string>
			<key>highLevelStatus</key><string>S</string>
			<key>schemeIdentifier-schemeName</key><string>SharedScheme</string>
			<key>signature</key><string>Build SharedScheme</string>
			<key>timeStartedRecording</key><real>` + fmt.Sprintf("%f", entryStart) + `</real>
			<key>timeStoppedRecording</key><real>` + fmt.Sprintf("%f", entryStop) + `</real>
		</dict>
`)
	}

	b.WriteString(`	</dict>
</dict>
</plist>`)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "LogStoreManifest.plist"), []byte(b.String()), 0o644))
}

func TestWatcher_scan_ThreeEntriesSameScheme_EmitsThreeGroups(t *testing.T) {
	// Without a MetricsLookup the default is one group per entry, so three
	// same-scheme Builds 10 s apart land as three independent groups.
	home := t.TempDir()
	derivedRoot := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-grp")
	writeMultiEntryManifest(t, derivedRoot, "u1", "u2", "u3")

	var groups []ManifestEntryGroup
	w := &Watcher{
		Now:     func() time.Time { return fixtureNow },
		HomeDir: home,
		Handle: func(_ string, g ManifestEntryGroup) {
			groups = append(groups, g)
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)

	require.Len(t, groups, 3, "one group per entry when no MetricsLookup is installed")
	assert.Equal(t, []string{"u1"}, groups[0].UUIDs())
	assert.Equal(t, []string{"u2"}, groups[1].UUIDs())
	assert.Equal(t, []string{"u3"}, groups[2].UUIDs())
	assert.Len(t, w.seen, 3, "every emitted group's UUID is marked seen")
}

func TestWatcher_scan_AppendingLateEntry_EmitsOnlyTheNewOne(t *testing.T) {
	// With one group per entry, each re-scan only emits groups whose Primary
	// UUID is not yet in the seen set — a late non-Primary entry joining a
	// scheme is a new group of its own, not a mutation of a prior group.
	home := t.TempDir()
	derivedRoot := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-append")
	writeMultiEntryManifest(t, derivedRoot, "u1", "u2")

	var handleCalls int
	var lastUUIDs []string
	w := &Watcher{
		Now:     func() time.Time { return fixtureNow },
		HomeDir: home,
		Handle: func(_ string, g ManifestEntryGroup) {
			handleCalls++
			lastUUIDs = g.UUIDs()
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)
	require.Equal(t, 2, handleCalls, "initial scan emits one group per entry")

	writeMultiEntryManifest(t, derivedRoot, "u1", "u2", "u3")

	w.scan(false, false)
	require.Equal(t, 3, handleCalls, "second scan emits only the newly-appended entry — prior UUIDs are seen")
	assert.Equal(t, []string{"u3"}, lastUUIDs)
}

// writeManifestWithEntries lets a test specify each entry's UUID / signature /
// start offset / stop offset in seconds — needed for primary-change scenarios
// where writeMultiEntryManifest's uniform "Build SharedScheme" fixture can't
// shift Primary.
type manifestEntryFixture struct {
	UUID         string
	Signature    string
	StartOffset  float64
	StopOffset   float64
	FileNameUUID string
}

func writeManifestWithEntries(t *testing.T, derivedRoot string, entries []manifestEntryFixture) {
	t.Helper()

	dir := filepath.Join(derivedRoot, "Logs", "Build")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>logs</key>
	<dict>
`)

	base := 762345900.0
	for _, e := range entries {
		fileUUID := e.FileNameUUID
		if fileUUID == "" {
			fileUUID = e.UUID
		}
		b.WriteString(`		<key>` + e.UUID + `</key>
		<dict>
			<key>fileName</key><string>` + fileUUID + `.xcactivitylog</string>
			<key>highLevelStatus</key><string>S</string>
			<key>schemeIdentifier-schemeName</key><string>SharedScheme</string>
			<key>signature</key><string>` + e.Signature + `</string>
			<key>timeStartedRecording</key><real>` + fmt.Sprintf("%f", base+e.StartOffset) + `</real>
			<key>timeStoppedRecording</key><real>` + fmt.Sprintf("%f", base+e.StopOffset) + `</real>
		</dict>
`)
	}

	b.WriteString(`	</dict>
</dict>
</plist>`)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "LogStoreManifest.plist"), []byte(b.String()), 0o644))
}

// TestWatcher_scan_NewPrimaryTriggersReemit exercises the per-entry default:
// adding a second entry (Archive outranks Build) produces a second group with
// its own Primary, which emits because its UUID is not yet in the seen set.
func TestWatcher_scan_NewPrimaryTriggersReemit(t *testing.T) {
	home := t.TempDir()
	derivedRoot := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-primary-change")

	writeManifestWithEntries(t, derivedRoot, []manifestEntryFixture{
		{UUID: "u-build", Signature: "Build SharedScheme", StartOffset: 0, StopOffset: 10},
	})

	var handleCalls int
	var lastPrimaryUUID string
	w := &Watcher{
		Now:     func() time.Time { return fixtureNow },
		HomeDir: home,
		Handle: func(_ string, g ManifestEntryGroup) {
			handleCalls++
			lastPrimaryUUID = g.Primary().UUID
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)
	require.Equal(t, 1, handleCalls, "initial scan emits build-anchored group")
	require.Equal(t, "u-build", lastPrimaryUUID)

	// Add an Archive entry — it lands as its own group under the per-entry
	// default and emits because its UUID is fresh.
	writeManifestWithEntries(t, derivedRoot, []manifestEntryFixture{
		{UUID: "u-build", Signature: "Build SharedScheme", StartOffset: 0, StopOffset: 10},
		{UUID: "u-archive", Signature: "Archive SharedScheme", StartOffset: 20, StopOffset: 40},
	})

	w.scan(false, false)
	require.Equal(t, 2, handleCalls, "the new Archive entry emits as its own group")
	assert.Equal(t, "u-archive", lastPrimaryUUID, "second emission is the Archive-anchored group")
}

func TestWatcher_scan_SkipsEntriesOlderThanHandledMaxAge(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)

	handles := 0

	w := &Watcher{
		HomeDir: home,
		Handle: func(string, ManifestEntryGroup) {
			handles++
		},
		Now:     func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) },
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)

	assert.Zero(t, handles, "entries older than HandledManifestMaxAge must be skipped, not resurrected as orphans")
	assert.Empty(t, w.seen, "stale entries must not be marked seen either")
}

// sidecarReaderStub is a minimal SidecarReader for watcher tests that only
// cares about the pending DD-path hint stream.
type sidecarReaderStub struct {
	ddPaths      []string
	pendingCount int
}

func (s *sidecarReaderStub) Lookup(ManifestEntryGroup, LookupFilter) (*blobstats.Snapshot, []string, string, bool) {
	return nil, nil, "", false
}

func (s *sidecarReaderStub) Has(ManifestEntryGroup, LookupFilter) bool { return false }

func (s *sidecarReaderStub) OrphanSidecarsGroupedByInvocation(_ time.Time, _ time.Duration) map[string]SidecarGroup {
	return nil
}

func (s *sidecarReaderStub) PendingDerivedDataPaths() []string { return s.ddPaths }

func (s *sidecarReaderStub) PendingCount() int {
	if s.pendingCount > 0 {
		return s.pendingCount
	}

	return len(s.ddPaths)
}

func TestWatcher_scan_SidecarDDPathIsGlobbed(t *testing.T) {
	home := t.TempDir()

	// Project-local DerivedData outside both HOME-relative defaults.
	projectDD := filepath.Join(t.TempDir(), "DerivedData")
	writeInlineManifest(t, projectDD, "Build", "UUID-PROJECT-LOCAL")

	// Fake SidecarReader hints the watcher at the project-local DD path.
	// Use the enrichment-package internal interface — must satisfy both Lookup
	// and PendingDerivedDataPaths; satisfied via sidecarReaderStub below.
	reader := &sidecarReaderStub{ddPaths: []string{projectDD}}

	var handled []string
	w := &Watcher{
		Now:           func() time.Time { return fixtureNow },
		HomeDir:       home,
		SidecarReader: reader,
		Handle: func(_ string, g ManifestEntryGroup) {
			handled = append(handled, g.UUIDs()...)
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)

	assert.Equal(t, []string{"UUID-PROJECT-LOCAL"}, handled,
		"sidecar-advertised project-local DD must be globbed on top of HOME-relative defaults")
}

// captureLogger buffers Debugf calls so tests can assert on emitted lines.
type captureLogger struct {
	lines []string
}

func (c *captureLogger) Debugf(fmtStr string, args ...any) {
	c.lines = append(c.lines, fmt.Sprintf(fmtStr, args...))
}

func (c *captureLogger) Errorf(fmtStr string, args ...any)  { c.Debugf(fmtStr, args...) }
func (c *captureLogger) Warnf(fmtStr string, args ...any)   { c.Debugf(fmtStr, args...) }
func (c *captureLogger) Infof(fmtStr string, args ...any)   { c.Debugf(fmtStr, args...) }
func (c *captureLogger) Printf(fmtStr string, args ...any)  { c.Debugf(fmtStr, args...) }
func (c *captureLogger) Donef(fmtStr string, args ...any)   { c.Debugf(fmtStr, args...) }
func (c *captureLogger) TErrorf(fmtStr string, args ...any) { c.Debugf(fmtStr, args...) }
func (c *captureLogger) TWarnf(fmtStr string, args ...any)  { c.Debugf(fmtStr, args...) }
func (c *captureLogger) TInfof(fmtStr string, args ...any)  { c.Debugf(fmtStr, args...) }
func (c *captureLogger) TPrintf(fmtStr string, args ...any) { c.Debugf(fmtStr, args...) }
func (c *captureLogger) TDebugf(fmtStr string, args ...any) { c.Debugf(fmtStr, args...) }
func (c *captureLogger) TDonef(fmtStr string, args ...any)  { c.Debugf(fmtStr, args...) }
func (c *captureLogger) Println()                           {}
func (c *captureLogger) EnableDebugLog(bool)                {}

func TestWatcher_scan_StalledDiagnostic_EmitsWhenZeroManifestsWithPendingSidecars(t *testing.T) {
	logger := &captureLogger{}
	home := t.TempDir()
	w := &Watcher{
		Now:           func() time.Time { return fixtureNow },
		HomeDir:       home,
		SidecarReader: &sidecarReaderStub{ddPaths: []string{"/project/DD"}, pendingCount: 3},
		Logger:        logger,
		Handle:        func(string, ManifestEntryGroup) {},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)

	var found bool
	for _, line := range logger.lines {
		if strings.Contains(line, "0 manifests") && strings.Contains(line, "/project/DD") {
			found = true

			break
		}
	}
	assert.True(t, found, "diagnostic debug line must fire; got %v", logger.lines)
}

func TestWatcher_scan_StalledDiagnostic_RateLimitedToOncePerMinute(t *testing.T) {
	logger := &captureLogger{}
	now := fixtureNow
	w := &Watcher{
		Now:           func() time.Time { return now },
		HomeDir:       t.TempDir(),
		SidecarReader: &sidecarReaderStub{ddPaths: []string{"/project/DD"}, pendingCount: 2},
		Logger:        logger,
		Handle:        func(string, ManifestEntryGroup) {},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)
	w.scan(false, false) // same-tick: must stay silent

	diagCount := 0
	for _, line := range logger.lines {
		if strings.Contains(line, "0 manifests") {
			diagCount++
		}
	}
	assert.Equal(t, 1, diagCount, "second scan at same timestamp must not re-emit (rate limited)")

	now = fixtureNow.Add(zeroMatchStallInterval + time.Second)
	w.scan(false, false)

	diagCount = 0
	for _, line := range logger.lines {
		if strings.Contains(line, "0 manifests") {
			diagCount++
		}
	}
	assert.Equal(t, 2, diagCount, "after > 1min, diag must re-fire")
}

func TestWatcher_scan_StalledDiagnostic_SilentWhenNoSidecars(t *testing.T) {
	logger := &captureLogger{}
	w := &Watcher{
		Now:           func() time.Time { return fixtureNow },
		HomeDir:       t.TempDir(),
		SidecarReader: &sidecarReaderStub{}, // zero sidecars
		Logger:        logger,
		Handle:        func(string, ManifestEntryGroup) {},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)

	for _, line := range logger.lines {
		assert.NotContains(t, line, "0 manifests",
			"no sidecars => no diagnostic (prevents noise on an idle machine)")
	}
}

func TestWatcher_scan_SidecarAbsentOrEmpty_NoExtraGlobs(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)

	w := &Watcher{
		Now:           func() time.Time { return fixtureNow },
		HomeDir:       home,
		SidecarReader: &sidecarReaderStub{ddPaths: nil}, // no hints
		Handle:        func(string, ManifestEntryGroup) {},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	assert.NotPanics(t, func() { w.scan(false, false) }, "nil hints must not break the scan")
}

func TestWatcher_ScanOnce_InitialisesMapsAndEmits(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)
	uuid := pickUUID(t, home)

	var handled []string
	w := &Watcher{
		Now:     func() time.Time { return fixtureNow },
		HomeDir: home,
		Handle: func(_ string, g ManifestEntryGroup) {
			handled = append(handled, g.UUIDs()...)
		},
	}

	w.ScanOnce()

	assert.Contains(t, handled, uuid, "ScanOnce must emit without a prior Run")
	assert.Contains(t, w.seen, uuid, "ScanOnce must lazy-init seen and mark emitted uuids")
}

// TestWatcher_ScanOnceWithEmit_InvokesObserver pins the flush-session shape:
// the external observer fires for every InvocationID the Enricher records via
// RecordEmittedInvocationID during the scan, and does NOT leak after the call.
func TestWatcher_ScanOnceWithEmit_InvokesObserver(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)
	uuid := pickUUID(t, home)

	var w *Watcher
	w = &Watcher{
		Now:     func() time.Time { return fixtureNow },
		HomeDir: home,
		Handle: func(_ string, g ManifestEntryGroup) {
			w.RecordEmittedInvocationID("inv-"+g.UUIDs()[0], "", g)
		},
	}

	var observed []string
	w.ScanOnceWithEmit(func(id string) {
		observed = append(observed, id)
	})

	assert.Contains(t, observed, "inv-"+uuid, "observer must fire for emitted IDs")

	observed = nil
	w.ScanOnce()
	assert.Empty(t, observed, "observer must not persist past ScanOnceWithEmit")
}

func TestWatcher_scanOrphanSidecars_FiresPseudoGroupForAgedOrphan(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	start := now.Add(-5 * time.Minute)
	stop := start.Add(45 * time.Second)

	reader := &stubReader{groups: map[string]SidecarGroup{
		"invocation-X": {Start: start, Stop: stop, SidecarUUIDs: []string{"sidecar-a"}},
	}}

	var handled []ManifestEntryGroup
	w := &Watcher{
		HomeDir:               home,
		Now:                   func() time.Time { return now },
		SidecarReader:         reader,
		MatchProbe:            func(ManifestEntryGroup) bool { return false },
		MaxCorrelationRetries: 6,
		Handle: func(path string, g ManifestEntryGroup) {
			assert.Equal(t, "", path, "synthetic path must be empty — no sibling xcactivitylog")
			handled = append(handled, g)
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)

	require.Len(t, handled, 1, "aged orphan sidecar must synthesize exactly one pseudo-group")
	assert.Equal(t, SyntheticManifestGroupUUIDPrefix+"sidecar-a", handled[0].Entries[0].UUID)
	assert.Equal(t, start, handled[0].Start())
	assert.Equal(t, stop, handled[0].Stop())
	assert.Equal(t, "build", handled[0].Command(), "no overlapping manifest emission → signature falls back to bare build")
	assert.Empty(t, handled[0].OverrideInvocationID(), "no manifest overlap → no override stamped")

	require.Len(t, reader.calls, 1)
	assert.Equal(t, now, reader.calls[0].now)
	assert.Equal(t, 2*DefaultPollInterval, reader.calls[0].minAge, "periodic tick must use 2*PollInterval as the age gate")
}

func TestWatcher_scanOrphanSidecars_ForceSynthesizeDropsAgeGate(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)

	reader := &stubReader{groups: map[string]SidecarGroup{
		"invocation-X": {Start: now.Add(-time.Second), Stop: now, SidecarUUIDs: []string{"sidecar-a"}},
	}}

	w := &Watcher{
		HomeDir:               home,
		Now:                   func() time.Time { return now },
		SidecarReader:         reader,
		MatchProbe:            func(ManifestEntryGroup) bool { return false },
		MaxCorrelationRetries: 6,
		Handle:                func(string, ManifestEntryGroup) {},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.ScanOnce()

	require.Len(t, reader.calls, 1)
	assert.Equal(t, time.Duration(0), reader.calls[0].minAge, "shutdown sweep must drop the age gate")
}

func TestWatcher_scanOrphanSidecars_SyntheticBypassesRetryBucket(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)

	reader := &stubReader{groups: map[string]SidecarGroup{
		"invocation-X": {Start: now.Add(-time.Minute), Stop: now.Add(-time.Second), SidecarUUIDs: []string{"sidecar-a"}},
	}}

	var handled int
	w := &Watcher{
		HomeDir:               home,
		Now:                   func() time.Time { return now },
		SidecarReader:         reader,
		MatchProbe:            func(ManifestEntryGroup) bool { return false },
		MaxCorrelationRetries: 6,
		Handle: func(string, ManifestEntryGroup) {
			handled++
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)

	assert.Equal(t, 1, handled, "synthetic group must bypass the retry bucket even when MatchProbe returns false")
	assert.Empty(t, w.retries, "no retry bucket entry for synthetic groups")
}

func TestWatcher_scanOrphanSidecars_SameTickDedupSkipsManifestEmittedID(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)

	reader := &stubReader{groups: map[string]SidecarGroup{
		"id-manifest": {Start: fixtureNow.Add(-time.Minute), Stop: fixtureNow.Add(-30 * time.Second), SidecarUUIDs: []string{"sidecar-a"}},
		"id-orphan":   {Start: fixtureNow.Add(-time.Minute), Stop: fixtureNow.Add(-30 * time.Second), SidecarUUIDs: []string{"sidecar-b"}},
	}}

	var handled []ManifestEntryGroup
	w := &Watcher{
		HomeDir:       home,
		Now:           func() time.Time { return fixtureNow },
		SidecarReader: reader,
		Handle: func(_ string, g ManifestEntryGroup) {
			handled = append(handled, g)
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	// Simulate the manifest-emission side: the enricher's OnEmit hook will
	// fire with "id-manifest" between the manifest loop and scanOrphanSidecars.
	// Here we approximate that by wrapping Handle to call RecordEmittedInvocationID.
	w.Handle = func(path string, g ManifestEntryGroup) {
		handled = append(handled, g)
		// Only the real manifest entries invoke the manifest-side side-effect; synthetic groups must not loop-feed the dedup set.
		if !strings.HasPrefix(g.Entries[0].UUID, SyntheticManifestGroupUUIDPrefix) {
			w.RecordEmittedInvocationID("id-manifest", "", g)
		}
	}

	w.scan(false, false)

	// Both manifest groups + one synthetic (id-orphan) must handle;
	// id-manifest's synthetic must be skipped.
	var synthetic []string
	for _, g := range handled {
		if strings.HasPrefix(g.Entries[0].UUID, SyntheticManifestGroupUUIDPrefix) {
			synthetic = append(synthetic, g.Entries[0].UUID)
		}
	}

	require.Len(t, synthetic, 1, "exactly one synthetic group must emit — the dedup set must skip id-manifest's")
	assert.Equal(t, SyntheticManifestGroupUUIDPrefix+"sidecar-b", synthetic[0], "the surviving synthetic group must be the one whose InvocationID was NOT emitted via the manifest path")
}

func TestWatcher_scanOrphanSidecars_EmitsEvenWhenManifestWindowOverlaps(t *testing.T) {
	// IDE flow regression: the manifest-path Lookup races with sidecar flush
	// (plugin stays connected until the IDE build teardown, long after the
	// manifest lands). If scanOrphanSidecars defers on time overlap, the
	// sidecar's wrapper InvocationID + blob stats never PUT — BE sees only
	// the empty orphan the manifest path minted. Overlap must not defer:
	// consumed sidecars are unlinked synchronously inside Handle, so a
	// sidecar still on disk here is one the manifest path did not consume.
	home := t.TempDir()
	derivedRoot := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-overlap")
	writeMultiEntryManifest(t, derivedRoot, "u1")
	manifestStart := time.Date(2025, 2, 27, 10, 45, 0, 0, time.UTC)
	manifestStop := time.Date(2025, 2, 27, 10, 45, 5, 0, time.UTC)

	reader := &stubReader{groups: map[string]SidecarGroup{
		"invocation-overlap": {
			Start:        manifestStart.Add(-time.Second),
			Stop:         manifestStop.Add(time.Second),
			SidecarUUIDs: []string{"sidecar-overlap"},
		},
		"invocation-orphan": {
			Start:        manifestStart.Add(-time.Hour),
			Stop:         manifestStart.Add(-time.Hour).Add(5 * time.Second),
			SidecarUUIDs: []string{"sidecar-orphan"},
		},
	}}

	var handled []ManifestEntryGroup
	w := &Watcher{
		HomeDir:       home,
		Now:           func() time.Time { return fixtureNow.Add(10 * time.Minute) },
		SidecarReader: reader,
		Handle: func(_ string, g ManifestEntryGroup) {
			handled = append(handled, g)
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)

	var synthetic, real_ []string
	for _, g := range handled {
		uuid := g.Entries[0].UUID
		if strings.HasPrefix(uuid, SyntheticManifestGroupUUIDPrefix) {
			synthetic = append(synthetic, uuid)
		} else {
			real_ = append(real_, uuid)
		}
	}

	assert.Equal(t, []string{"u1"}, real_, "the real manifest group must emit")
	assert.ElementsMatch(t,
		[]string{SyntheticManifestGroupUUIDPrefix + "sidecar-overlap", SyntheticManifestGroupUUIDPrefix + "sidecar-orphan"},
		synthetic,
		"both sidecars must emit — overlap no longer defers",
	)
}

func TestWatcher_scanOrphanSidecars_ShutdownSweepIgnoresOverlapDefer(t *testing.T) {
	// Shutdown sweep (ScanOnce → forceSynthesize=true) must land every
	// orphan sidecar even if a manifest time-overlaps — a Lookup failure
	// earlier in this scan may have left the sidecar on disk, and this is
	// the last chance to PUT its data before the proxy exits.
	home := t.TempDir()
	derivedRoot := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-shutdown")
	writeMultiEntryManifest(t, derivedRoot, "u1")
	manifestStart := time.Date(2025, 2, 27, 12, 5, 0, 0, time.UTC)
	manifestStop := time.Date(2025, 2, 27, 12, 5, 5, 0, time.UTC)

	reader := &stubReader{groups: map[string]SidecarGroup{
		"invocation-overlap": {
			Start:        manifestStart.Add(-time.Second),
			Stop:         manifestStop.Add(time.Second),
			SidecarUUIDs: []string{"sidecar-overlap"},
		},
	}}

	var synthetic int
	w := &Watcher{
		HomeDir:       home,
		Now:           func() time.Time { return fixtureNow.Add(10 * time.Minute) },
		SidecarReader: reader,
		Handle: func(_ string, g ManifestEntryGroup) {
			if strings.HasPrefix(g.Entries[0].UUID, SyntheticManifestGroupUUIDPrefix) {
				synthetic++
			}
		},
	}

	w.ScanOnce()

	assert.Equal(t, 1, synthetic, "shutdown sweep must fire the overlapping pseudo-group — last chance to land its data")
}

func TestWatcher_scanOrphanSidecars_NoReaderNoSynthesis(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)

	w := &Watcher{
		HomeDir: home,
		Now:     func() time.Time { return now },
		Handle: func(string, ManifestEntryGroup) {
			t.Fatal("no reader must produce no synthesis")
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)
}

type stubReader struct {
	groups map[string]SidecarGroup
	calls  []stubReaderCall
}

type stubReaderCall struct {
	now    time.Time
	minAge time.Duration
}

func (s *stubReader) Lookup(ManifestEntryGroup, LookupFilter) (*blobstats.Snapshot, []string, string, bool) {
	return nil, nil, "", false
}

func (s *stubReader) Has(ManifestEntryGroup, LookupFilter) bool { return false }

func (s *stubReader) OrphanSidecarsGroupedByInvocation(now time.Time, minAge time.Duration) map[string]SidecarGroup {
	s.calls = append(s.calls, stubReaderCall{now: now, minAge: minAge})

	return s.groups
}

func (s *stubReader) PendingDerivedDataPaths() []string { return nil }

func (s *stubReader) PendingCount() int { return 0 }

// TestWatcher_scanOrphanSidecars_SkipsPseudoGroup_WhenManifestAlreadyEmittedUnderSameInvocationID
// pins the clobber-safe path: a pseudo-group under the SAME InvocationID a
// prior manifest emission already PUT must NOT re-PUT. BE is last-write-wins,
// so a late-wave pseudo-group's partial hitRate would overwrite the
// authoritative manifest row for the same build.
func TestWatcher_scanOrphanSidecars_SkipsPseudoGroup_WhenManifestAlreadyEmittedUnderSameInvocationID(t *testing.T) {
	home := t.TempDir()
	derivedRoot := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-id-dedup")
	writeMultiEntryManifest(t, derivedRoot, "u-build")

	sidecarStart := time.Date(2025, 2, 27, 10, 45, 0, 0, time.UTC)
	sidecarStop := time.Date(2025, 2, 27, 10, 45, 10, 0, time.UTC)

	// The sidecar's InvocationID matches the one the manifest-orphan will PUT.
	const sharedID = "build-manifest-orphan"

	reader := &stubReader{groups: map[string]SidecarGroup{
		sharedID: {Start: sidecarStart, Stop: sidecarStop, SidecarUUIDs: []string{"sidecar-overlap"}},
	}}

	var handled []ManifestEntryGroup
	w := &Watcher{
		HomeDir:               home,
		Now:                   func() time.Time { return fixtureNow },
		SidecarReader:         reader,
		MatchProbe:            func(ManifestEntryGroup) bool { return false }, // force manifest-orphan mint
		MaxCorrelationRetries: 0,                                              // skip retry bucket so the mint fires immediately
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.Handle = func(_ string, g ManifestEntryGroup) {
		handled = append(handled, g)
		// Only the real manifest group feeds the dedup set — the pseudo-group
		// that comes later must not re-populate it.
		if !strings.HasPrefix(g.Entries[0].UUID, SyntheticManifestGroupUUIDPrefix) {
			w.RecordEmittedInvocationID(sharedID, "", g)
		}
	}

	w.scan(false, false)

	require.Len(t, handled, 1,
		"only the real manifest group must emit — the same-ID pseudo-group is skipped so its partial stats don't clobber the manifest row")
	assert.False(t, strings.HasPrefix(handled[0].Entries[0].UUID, SyntheticManifestGroupUUIDPrefix),
		"the single emission must be the real manifest group, not a pseudo-group")
}

// TestWatcher_scanOrphanSidecars_FreshInvocationID_EmitsEvenOnTimeOverlap pins
// the reseat-flow contract: when the proxy reseats under a new ⌘B the sidecar
// carries a distinct InvocationID, and the pseudo-group must emit even if its
// window overlaps the previously-emitted build's window. ID-match (not time
// overlap) is the only clobber discriminator.
func TestWatcher_scanOrphanSidecars_FreshInvocationID_EmitsEvenOnTimeOverlap(t *testing.T) {
	home := t.TempDir()
	derivedRoot := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-fresh-id")
	writeMultiEntryManifest(t, derivedRoot, "u-build")

	// The sidecar window falls inside the recentEmissions entry's window but the
	// InvocationID differs — the proxy reseated on a fresh ⌘B.
	sidecarStart := time.Date(2025, 2, 27, 10, 45, 0, 0, time.UTC)
	sidecarStop := time.Date(2025, 2, 27, 10, 45, 10, 0, time.UTC)

	reader := &stubReader{groups: map[string]SidecarGroup{
		"new-build-id": {Start: sidecarStart, Stop: sidecarStop, SidecarUUIDs: []string{"sidecar-new"}},
	}}

	var handled []ManifestEntryGroup
	w := &Watcher{
		HomeDir:               home,
		Now:                   func() time.Time { return fixtureNow },
		SidecarReader:         reader,
		MatchProbe:            func(ManifestEntryGroup) bool { return false },
		MaxCorrelationRetries: 0,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.Handle = func(_ string, g ManifestEntryGroup) {
		handled = append(handled, g)
		if !strings.HasPrefix(g.Entries[0].UUID, SyntheticManifestGroupUUIDPrefix) {
			// The previously-emitted build sits in recentEmissions under its OWN
			// InvocationID, different from the sidecar's.
			w.RecordEmittedInvocationID("old-build-id", "", g)
		}
	}

	w.scan(false, false)

	var synthetic []string
	for _, g := range handled {
		if strings.HasPrefix(g.Entries[0].UUID, SyntheticManifestGroupUUIDPrefix) {
			synthetic = append(synthetic, g.Entries[0].UUID)
		}
	}
	require.Len(t, synthetic, 1,
		"fresh InvocationID → pseudo-group emits even with time overlap (reseated ⌘B is a distinct BE row)")
	assert.Equal(t, SyntheticManifestGroupUUIDPrefix+"sidecar-new", synthetic[0])
}

// TestWatcher_scanOrphanSidecars_FallsBackToBareBuild_WhenNoOverlap: a
// non-overlapping sidecar emits a bare-build pseudo-group with no override.
func TestWatcher_scanOrphanSidecars_FallsBackToBareBuild_WhenNoOverlap(t *testing.T) {
	home := t.TempDir()
	derivedRoot := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-nooverlap")
	writeMultiEntryManifest(t, derivedRoot, "u-build")

	// Sidecar starts hours before the manifest entries — guaranteed no overlap.
	sidecarStart := time.Date(2025, 2, 27, 5, 0, 0, 0, time.UTC)
	sidecarStop := time.Date(2025, 2, 27, 5, 0, 10, 0, time.UTC)

	reader := &stubReader{groups: map[string]SidecarGroup{
		"orphan-id": {Start: sidecarStart, Stop: sidecarStop, SidecarUUIDs: []string{"sidecar-orphan"}},
	}}

	var pseudo *ManifestEntryGroup
	w := &Watcher{
		HomeDir:               home,
		Now:                   func() time.Time { return fixtureNow },
		SidecarReader:         reader,
		MatchProbe:            func(ManifestEntryGroup) bool { return false },
		MaxCorrelationRetries: 0,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}
	w.Handle = func(_ string, g ManifestEntryGroup) {
		if strings.HasPrefix(g.Entries[0].UUID, SyntheticManifestGroupUUIDPrefix) {
			pseudo = &g
		} else {
			w.RecordEmittedInvocationID("build-orphan-elsewhere", "", g)
		}
	}

	w.scan(false, false)

	require.NotNil(t, pseudo, "pseudo-group must emit even without an overlapping manifest")
	assert.Empty(t, pseudo.OverrideInvocationID(), "no overlap → no override → enricher uses the sidecar's wrapper InvocationID")
	assert.Equal(t, "build", pseudo.Command(), "no scheme hint → bare build rendering")
}

// TestWatcher_scanOrphanSidecars_ShutdownSweepStaysSidecarAnchored:
// forceSynthesize=true skips recentEmissions so a late sidecar keeps its own
// InvocationID even when a manifest group overlaps.
func TestWatcher_scanOrphanSidecars_ShutdownSweepStaysSidecarAnchored(t *testing.T) {
	home := t.TempDir()
	derivedRoot := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-shutdown-anchor")
	writeMultiEntryManifest(t, derivedRoot, "u-build")

	sidecarStart := time.Date(2025, 2, 27, 10, 45, 0, 0, time.UTC)
	sidecarStop := time.Date(2025, 2, 27, 10, 45, 10, 0, time.UTC)

	reader := &stubReader{groups: map[string]SidecarGroup{
		"sidecar-id": {Start: sidecarStart, Stop: sidecarStop, SidecarUUIDs: []string{"sidecar-shutdown"}},
	}}

	var pseudo *ManifestEntryGroup
	w := &Watcher{
		HomeDir:       home,
		Now:           func() time.Time { return fixtureNow },
		SidecarReader: reader,
	}
	w.Handle = func(_ string, g ManifestEntryGroup) {
		if strings.HasPrefix(g.Entries[0].UUID, SyntheticManifestGroupUUIDPrefix) {
			pseudo = &g
		} else {
			w.RecordEmittedInvocationID("would-have-been-override", "", g)
		}
	}

	w.ScanOnce()

	require.NotNil(t, pseudo, "shutdown sweep must emit the pseudo-group")
	assert.Empty(t, pseudo.OverrideInvocationID(),
		"shutdown sweep (forceSynthesize=true) must NOT reuse manifest-orphan ID — sidecar stays anchored to its own invocation")
}

// TestWatcher_scanOrphanSidecars_CrossTickDedup_WithinRetention: IDE flow
// mints the manifest-orphan in tick N and flushes the sidecar in tick N+1 —
// the late pseudo-group must NOT PUT. Watcher-owned recentEmissions retains
// the manifest ID across the tick boundary, so the late pseudo-group's
// partial stats can't clobber the authoritative manifest row.
func TestWatcher_scanOrphanSidecars_CrossTickDedup_WithinRetention(t *testing.T) {
	home := t.TempDir()
	derivedRoot := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-cross-tick")
	writeMultiEntryManifest(t, derivedRoot, "u-build")

	// Sidecar window overlaps the manifest entries (~10:45 UTC on 2025-02-27).
	sidecarStart := time.Date(2025, 2, 27, 10, 45, 0, 0, time.UTC)
	sidecarStop := time.Date(2025, 2, 27, 10, 45, 10, 0, time.UTC)

	const sharedID = "build-manifest-orphan"

	// stagedReader returns no sidecar groups on the first call (tick N) and the
	// orphan group on subsequent calls (tick N+1) — simulating the IDE flow
	// where sidecar flush is deferred past the manifest-orphan mint. The late
	// sidecar carries the SAME InvocationID the manifest already PUT.
	reader := &stagedReader{stages: []map[string]SidecarGroup{
		nil,
		{sharedID: {Start: sidecarStart, Stop: sidecarStop, SidecarUUIDs: []string{"sidecar-overlap"}}},
	}}

	// Clock advances less than RecentEmissionsRetention between ticks.
	tickTimes := []time.Time{fixtureNow, fixtureNow.Add(RecentEmissionsRetention / 2)}
	var tickIdx int
	nowFn := func() time.Time { return tickTimes[tickIdx] }

	var handled []ManifestEntryGroup
	w := &Watcher{
		HomeDir:               home,
		Now:                   nowFn,
		SidecarReader:         reader,
		MatchProbe:            func(ManifestEntryGroup) bool { return false },
		MaxCorrelationRetries: 0,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.Handle = func(_ string, g ManifestEntryGroup) {
		handled = append(handled, g)
		if !strings.HasPrefix(g.Entries[0].UUID, SyntheticManifestGroupUUIDPrefix) {
			w.RecordEmittedInvocationID(sharedID, "", g)
		}
	}

	// Tick N: manifest-orphan mints; stubReader returns no sidecars.
	w.scan(false, false)
	require.Len(t, handled, 1, "tick N: only the manifest-orphan emits")
	require.False(t, strings.HasPrefix(handled[0].Entries[0].UUID, SyntheticManifestGroupUUIDPrefix))

	// Tick N+1: sidecar flushes; manifest entries already seen.
	tickIdx = 1
	w.scan(false, false)

	require.Len(t, handled, 1,
		"tick N+1: late-wave pseudo-group must be skipped — recentEmissions retains the manifest ID across ticks so its partial stats don't clobber the manifest row")
}

// TestWatcher_scanOrphanSidecars_CrossTickDedup_ExpiresAfterRetention: an
// emission older than RecentEmissionsRetention is pruned, so a late sidecar
// falls back to its own InvocationID instead of inheriting the stale ID.
func TestWatcher_scanOrphanSidecars_CrossTickDedup_ExpiresAfterRetention(t *testing.T) {
	home := t.TempDir()
	derivedRoot := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-expired")
	writeMultiEntryManifest(t, derivedRoot, "u-build")

	sidecarStart := time.Date(2025, 2, 27, 10, 45, 0, 0, time.UTC)
	sidecarStop := time.Date(2025, 2, 27, 10, 45, 10, 0, time.UTC)

	const sharedID = "stale-manifest-id"

	reader := &stagedReader{stages: []map[string]SidecarGroup{
		nil,
		{sharedID: {Start: sidecarStart, Stop: sidecarStop, SidecarUUIDs: []string{"sidecar-late"}}},
	}}

	// Second tick lands past the retention window — the emission must be pruned.
	tickTimes := []time.Time{fixtureNow, fixtureNow.Add(RecentEmissionsRetention + time.Minute)}
	var tickIdx int

	var handled []ManifestEntryGroup
	w := &Watcher{
		HomeDir:               home,
		Now:                   func() time.Time { return tickTimes[tickIdx] },
		SidecarReader:         reader,
		MatchProbe:            func(ManifestEntryGroup) bool { return false },
		MaxCorrelationRetries: 0,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}
	w.Handle = func(_ string, g ManifestEntryGroup) {
		handled = append(handled, g)
		if !strings.HasPrefix(g.Entries[0].UUID, SyntheticManifestGroupUUIDPrefix) {
			w.RecordEmittedInvocationID(sharedID, "", g)
		}
	}

	w.scan(false, false)
	require.Len(t, handled, 1)

	tickIdx = 1
	w.scan(false, false)

	require.Len(t, handled, 2,
		"tick N+1: pseudo-group must emit — stale recentEmissions entry was pruned at scan start so the skip gate no longer trips")
	pseudo := handled[1]
	require.True(t, strings.HasPrefix(pseudo.Entries[0].UUID, SyntheticManifestGroupUUIDPrefix))
	assert.Empty(t, pseudo.OverrideInvocationID(),
		"stale manifest emission must be pruned at scan start — pseudo-group falls back to its own InvocationID")
}

// stagedReader returns a different group map per invocation of
// OrphanSidecarsGroupedByInvocation; the Nth call returns stages[N] (clamped
// to the final entry). Lets tests simulate per-tick sidecar visibility.
type stagedReader struct {
	stages [](map[string]SidecarGroup)
	calls  int
}

func (s *stagedReader) Lookup(ManifestEntryGroup, LookupFilter) (*blobstats.Snapshot, []string, string, bool) {
	return nil, nil, "", false
}

func (s *stagedReader) Has(ManifestEntryGroup, LookupFilter) bool { return false }

func (s *stagedReader) OrphanSidecarsGroupedByInvocation(time.Time, time.Duration) map[string]SidecarGroup {
	idx := s.calls
	if idx >= len(s.stages) {
		idx = len(s.stages) - 1
	}
	s.calls++

	return s.stages[idx]
}

func (s *stagedReader) PendingDerivedDataPaths() []string { return nil }

func (s *stagedReader) PendingCount() int { return 0 }

func TestWatcher_ScanOnce_PreservesExistingState(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)

	var handled []string
	w := &Watcher{
		Now:     func() time.Time { return fixtureNow },
		HomeDir: home,
		Handle: func(_ string, g ManifestEntryGroup) {
			handled = append(handled, g.UUIDs()...)
		},
	}

	w.ScanOnce()
	require.NotEmpty(t, handled, "first ScanOnce emits the fixture entries")

	before := append([]string(nil), handled...)
	w.ScanOnce()

	assert.Equal(t, before, handled, "subsequent ScanOnce must not re-emit already-seen uuids")
}

// hasControllingStub is a SidecarReader whose Has response is driven by a
// per-scan atomic — tests flip the flag between scans to simulate a sidecar
// materialising mid-retry-bucket.
type hasControllingStub struct {
	hasFn func() bool
}

func (s *hasControllingStub) Lookup(ManifestEntryGroup, LookupFilter) (*blobstats.Snapshot, []string, string, bool) {
	return nil, nil, "", false
}

func (s *hasControllingStub) Has(ManifestEntryGroup, LookupFilter) bool {
	if s.hasFn == nil {
		return false
	}

	return s.hasFn()
}

func (s *hasControllingStub) OrphanSidecarsGroupedByInvocation(time.Time, time.Duration) map[string]SidecarGroup {
	return nil
}

func (s *hasControllingStub) PendingDerivedDataPaths() []string { return nil }
func (s *hasControllingStub) PendingCount() int                 { return 0 }

// TestWatcher_RetryBucket_FiresEarlyWhenSidecarArrives pins the convergence
// contract: a group buckets for retry on tick 1 (no sidecar yet, MatchProbe
// unmatched). Between ticks a sidecar flushes to disk. On the next scan tick
// Has returns true, and the retry-bucket resolves early via SidecarReader.Has
// — before MaxCorrelationRetries exhausts and the orphan mint fires.
func TestWatcher_RetryBucket_FiresEarlyWhenSidecarArrives(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)
	uuid := pickUUID(t, home)

	var sidecarPresent bool
	reader := &hasControllingStub{hasFn: func() bool { return sidecarPresent }}

	var handled []string
	w := &Watcher{
		Now:           func() time.Time { return fixtureNow },
		HomeDir:       home,
		SidecarReader: reader,
		Handle: func(_ string, g ManifestEntryGroup) {
			if groupContainsUUID(g, uuid) {
				handled = append(handled, uuid)
			}
		},
		// Pending-record probe never matches — only the sidecar signal can resolve.
		MatchProbe:            func(ManifestEntryGroup) bool { return false },
		MaxCorrelationRetries: 6,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	// Tick 1 — manifest fresh, no sidecar yet. Buckets for retry.
	w.scan(false, false)
	assert.Empty(t, handled, "first scan buckets the group (no sidecar yet)")
	assert.NotEmpty(t, w.retries, "retry bucket must contain the held group")

	// Tick 2 — still no sidecar. Decrements.
	w.scan(false, false)
	assert.Empty(t, handled, "second scan still held")

	// Sidecar materialises on disk between ticks 2 and 3.
	sidecarPresent = true

	// Tick 3 — Has returns true; retry bucket must resolve this tick, well
	// before MaxCorrelationRetries exhausts.
	w.scan(false, false)
	assert.Equal(t, []string{uuid}, handled,
		"sidecar presence must resolve the retry bucket on the first tick after it lands")
	assert.Contains(t, w.seen, uuid)
}

// TestWatcher_FirstPass_FiresWhenSidecarAlreadyPresent pins the fast path:
// sidecar is on disk BEFORE the manifest surfaces, so the first-pass
// canCorrelate call succeeds and the group fires without ever opening the
// retry bucket.
func TestWatcher_FirstPass_FiresWhenSidecarAlreadyPresent(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)
	uuid := pickUUID(t, home)

	// Sidecar sits on disk already — Has returns true on the first probe.
	reader := &hasControllingStub{hasFn: func() bool { return true }}

	var handled []string
	w := &Watcher{
		Now:                   func() time.Time { return fixtureNow },
		HomeDir:               home,
		SidecarReader:         reader,
		MatchProbe:            func(ManifestEntryGroup) bool { return false },
		MaxCorrelationRetries: 6,
		Handle: func(_ string, g ManifestEntryGroup) {
			if groupContainsUUID(g, uuid) {
				handled = append(handled, uuid)
			}
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false, false)

	assert.Equal(t, []string{uuid}, handled, "first-pass must fire immediately when sidecar is already present")
	assert.Empty(t, w.retries, "no retry bucket when first-pass canCorrelate succeeds")
	assert.Contains(t, w.seen, uuid)
}

func TestCanCorrelate_BothWiredBothFalse(t *testing.T) {
	w := &Watcher{
		MatchProbe:    func(ManifestEntryGroup) bool { return false },
		SidecarReader: &hasControllingStub{hasFn: func() bool { return false }},
	}

	assert.False(t, w.canCorrelate("", ManifestEntryGroup{}))
}

// TestWatcher_WrapperFlow_MatchProbeOwnsResolution_WhenSidecarReaderNil guards against
// hasCorrelationSignal() regressing the wrapper flow when MatchProbe alone owns the row.
func TestWatcher_WrapperFlow_MatchProbeOwnsResolution_WhenSidecarReaderNil(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)
	uuid := pickUUID(t, home)

	var probeMatches bool

	var handled []string
	w := &Watcher{
		Now:                   func() time.Time { return fixtureNow },
		HomeDir:               home,
		SidecarReader:         nil,
		MatchProbe:            func(ManifestEntryGroup) bool { return probeMatches },
		MaxCorrelationRetries: 6,
		Handle: func(_ string, g ManifestEntryGroup) {
			if groupContainsUUID(g, uuid) {
				handled = append(handled, uuid)
			}
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	// Tick 1 — probe unmatched. Retry bucket opens on MatchProbe signal (nil SidecarReader
	// must not block hasCorrelationSignal).
	w.scan(false, false)
	assert.Empty(t, handled, "first scan buckets the group (probe not matched yet)")
	require.NotEmpty(t, w.retries, "retry bucket must open on MatchProbe signal even with nil SidecarReader")

	// Tick 2 — probe matches. Retry bucket resolves via MatchProbe alone.
	probeMatches = true
	w.scan(false, false)
	assert.Equal(t, []string{uuid}, handled, "MatchProbe must resolve retry bucket when SidecarReader is nil")
	assert.Contains(t, w.seen, uuid)
}

// TestWatcher_scanOrphanSidecars_HexMatch_ClonesManifestGroup pins the
// hex-bridge path: a sidecar's BuildRequestHex matches a recent manifest
// emission's recorded hash, so the pseudo-group is cloned off the manifest
// group and PUTs under the manifest's UUID with the sidecar blob stats
// preattached.
func TestWatcher_scanOrphanSidecars_HexMatch_ClonesManifestGroup(t *testing.T) {
	const hex = "ea6297e079ee1683905d008d83044b10"
	const manifestUUID = "manifest-uuid-u1"
	const proxyID = "proxy-derived-id"

	sidecarBlob := &blobstats.Snapshot{}
	sidecarBlob.Download.OpCount = 5
	sidecarBlob.Download.MissCount = 2

	reader := &stubReader{groups: map[string]SidecarGroup{
		proxyID: {
			Start:           fixtureNow.Add(-5 * time.Minute),
			Stop:            fixtureNow.Add(-4 * time.Minute),
			SidecarUUIDs:    []string{"sidecar-a"},
			Paths:           []string{"/tmp/sidecar-a.json"},
			Blob:            sidecarBlob,
			BuildRequestHex: hex,
		},
	}}

	var handled []ManifestEntryGroup
	w := &Watcher{
		HomeDir:       t.TempDir(),
		Now:           func() time.Time { return fixtureNow },
		SidecarReader: reader,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}
	w.recentEmissions = map[string]emissionRecord{}
	w.Handle = func(_ string, g ManifestEntryGroup) {
		handled = append(handled, g)
	}

	// Seed a recent manifest emission under manifestUUID with the matching hex.
	manifestGroup := ManifestEntryGroup{
		Entries: []ManifestEntry{{
			UUID:       manifestUUID,
			Signature:  "Build S",
			SchemeName: "S",
			Start:      fixtureNow.Add(-6 * time.Minute),
			Stop:       fixtureNow.Add(-5 * time.Minute),
		}},
	}
	w.RecordEmittedInvocationID("manifest-id", hex, manifestGroup)

	w.scanOrphanSidecars(false)

	require.Len(t, handled, 1, "hex match must emit exactly one cloned group")
	clone := handled[0]
	assert.Equal(t, manifestUUID, clone.ManifestUUID(), "clone keeps manifest UUID so enricher PUTs under it")

	blob, paths, ok := clone.Preattached()
	require.True(t, ok, "clone must carry a preattached sidecar payload")
	assert.Equal(t, sidecarBlob, blob, "preattached blob is the sidecar snapshot")
	assert.Equal(t, []string{"/tmp/sidecar-a.json"}, paths, "preattached consumedPaths are the sidecar's own paths")
}

// TestWatcher_scanOrphanSidecars_HexMiss_EmitsSeparateRow pins the safe
// default: when the sidecar's hex does not match any recent manifest
// emission, the pseudo-group falls through to the slim synthetic path under
// the proxy-derived InvocationID — separate BE row, no clobber risk.
func TestWatcher_scanOrphanSidecars_HexMiss_EmitsSeparateRow(t *testing.T) {
	const sidecarHex = "ea6297e079ee1683905d008d83044b10"
	const manifestHex = "ffffffffffffffffffffffffffffffff"
	const proxyID = "proxy-derived-id"

	reader := &stubReader{groups: map[string]SidecarGroup{
		proxyID: {
			Start:           fixtureNow.Add(-5 * time.Minute),
			Stop:            fixtureNow.Add(-4 * time.Minute),
			SidecarUUIDs:    []string{"sidecar-b"},
			BuildRequestHex: sidecarHex,
		},
	}}

	var handled []ManifestEntryGroup
	w := &Watcher{
		HomeDir:       t.TempDir(),
		Now:           func() time.Time { return fixtureNow },
		SidecarReader: reader,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}
	w.recentEmissions = map[string]emissionRecord{}
	w.Handle = func(_ string, g ManifestEntryGroup) {
		handled = append(handled, g)
	}

	// Seed a recent emission with a DIFFERENT hex — no bridge.
	manifestGroup := ManifestEntryGroup{
		Entries: []ManifestEntry{{
			UUID:      "manifest-uuid-other",
			Signature: "Build S",
			Start:     fixtureNow.Add(-6 * time.Minute),
			Stop:      fixtureNow.Add(-5 * time.Minute),
		}},
	}
	w.RecordEmittedInvocationID("manifest-id", manifestHex, manifestGroup)

	w.scanOrphanSidecars(false)

	require.Len(t, handled, 1, "hex mismatch must still emit — via the slim synthetic path")
	g := handled[0]
	assert.True(t, strings.HasPrefix(g.Entries[0].UUID, SyntheticManifestGroupUUIDPrefix), "fallback is the slim synthetic pseudo-group")
	_, _, preattached := g.Preattached()
	assert.False(t, preattached, "slim fallback must NOT carry a preattached payload — SidecarReader.Lookup will supply it")
}

// TestWatcher_findEmissionByBuildRequestHex_MultipleMatches_PicksClosestByStart
// pins the tie-breaker: when two emissions share the same hex (incremental
// rebuilds reused the build-description), the one whose group Start is
// closest to the sidecar's AcceptedAt wins — temporal proximity defeats a
// stale emission.
func TestWatcher_findEmissionByBuildRequestHex_MultipleMatches_PicksClosestByStart(t *testing.T) {
	const hex = "ea6297e079ee1683905d008d83044b10"

	w := &Watcher{Now: func() time.Time { return fixtureNow }}
	w.recentEmissions = map[string]emissionRecord{}

	farGroup := ManifestEntryGroup{
		Entries: []ManifestEntry{{
			UUID:  "manifest-far",
			Start: fixtureNow.Add(-10 * time.Minute),
			Stop:  fixtureNow.Add(-9 * time.Minute),
		}},
	}
	nearGroup := ManifestEntryGroup{
		Entries: []ManifestEntry{{
			UUID:  "manifest-near",
			Start: fixtureNow.Add(-2 * time.Minute),
			Stop:  fixtureNow.Add(-1 * time.Minute),
		}},
	}

	w.RecordEmittedInvocationID("id-far", hex, farGroup)
	w.RecordEmittedInvocationID("id-near", hex, nearGroup)

	sidecarAcceptedAt := fixtureNow.Add(-90 * time.Second)
	rec, ok := w.findEmissionByBuildRequestHex(hex, sidecarAcceptedAt)
	require.True(t, ok)
	assert.Equal(t, "manifest-near", rec.group.ManifestUUID(), "nearer Start must win the tie-break")
}

// TestWatcher_scanOrphanSidecars_HexMatch_SkipsCloneWhenManifestAlreadyPut
// pins the post-ladder-swap behavior: when the manifest path has already
// PUT under ManifestUUID (OnEmit populated recentEmissions[ManifestUUID]),
// scanOrphan must NOT re-PUT a clone — BE last-write-wins would clobber
// Path A's blob stats. Residual sidecars fall through to the slim
// pseudo-group path under their proxy-derived InvocationID instead.
func TestWatcher_scanOrphanSidecars_HexMatch_SkipsCloneWhenManifestAlreadyPut(t *testing.T) {
	const hex = "ea6297e079ee1683905d008d83044b10"
	const manifestUUID = "manifest-uuid-u1"
	const proxyID = "proxy-derived-id"

	sidecarBlob := &blobstats.Snapshot{}
	sidecarBlob.Download.OpCount = 5

	reader := &stubReader{groups: map[string]SidecarGroup{
		proxyID: {
			Start:           fixtureNow.Add(-5 * time.Minute),
			Stop:            fixtureNow.Add(-4 * time.Minute),
			SidecarUUIDs:    []string{"sidecar-residual"},
			Paths:           []string{"/tmp/sidecar-residual.json"},
			Blob:            sidecarBlob,
			BuildRequestHex: hex,
		},
	}}

	var handled []ManifestEntryGroup
	w := &Watcher{
		HomeDir:       t.TempDir(),
		Now:           func() time.Time { return fixtureNow },
		SidecarReader: reader,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}
	w.recentEmissions = map[string]emissionRecord{}
	w.Handle = func(_ string, g ManifestEntryGroup) {
		handled = append(handled, g)
	}

	manifestGroup := ManifestEntryGroup{
		Entries: []ManifestEntry{{
			UUID:       manifestUUID,
			Signature:  "Build S",
			SchemeName: "S",
			Start:      fixtureNow.Add(-6 * time.Minute),
			Stop:       fixtureNow.Add(-5 * time.Minute),
		}},
	}
	// Post-ladder-swap: Enrich PUTs under ManifestUUID, OnEmit keys recentEmissions on ManifestUUID.
	w.RecordEmittedInvocationID(manifestUUID, hex, manifestGroup)

	w.scanOrphanSidecars(false)

	require.Len(t, handled, 1, "residual sidecar must fall through to the slim pseudo-group path, not re-PUT a clone")
	got := handled[0]
	assert.True(t, strings.HasPrefix(got.Entries[0].UUID, SyntheticManifestGroupUUIDPrefix), "fallback must be slim synthetic, not a manifest clone")
	assert.Empty(t, got.ManifestUUID(), "fallback must NOT carry the manifest UUID — that would re-PUT under same ID")
}
