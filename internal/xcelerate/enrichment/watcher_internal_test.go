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

// nonEmptySidecarBlob returns a Snapshot with a single Download op so it
// satisfies scanOrphanSidecars' zero-blob suppression gate. Tests that pin
// the emission flow (age-gating, dedup, overlap, etc.) attach this to
// SidecarGroup.Blob; tests that specifically pin the zero-blob suppression
// path leave Blob nil or hand in an empty Snapshot.
func nonEmptySidecarBlob() *blobstats.Snapshot {
	b := &blobstats.Snapshot{}
	b.Download.OpCount = 1

	return b
}

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
			w.RecordEmittedInvocationID("inv-"+g.UUIDs()[0], "", g, nil)
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

