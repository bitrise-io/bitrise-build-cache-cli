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
		Handle: func(g ManifestEntryGroup) {
			calls++
			uuids = append(uuids, g.UUIDs()...)
		},
		MatchProbe:            func(ManifestEntryGroup) bool { return true },
		MaxCorrelationRetries: 3,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false)

	// Fixture has three schemes (three groups); every group fires and every UUID lands in seen.
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
		Handle: func(g ManifestEntryGroup) {
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
	w.scan(false)
	assert.Empty(t, handled, "held on first scan while MatchProbe still false")

	scans = 2
	w.scan(false)
	assert.Empty(t, handled, "still held on second scan")

	scans = 3
	w.scan(false)
	assert.Equal(t, []string{uuid}, handled, "fires on the scan MatchProbe first returns true")
	assert.Contains(t, w.seen, uuid)
}

// groupContainsUUID is a tiny helper for tests that pick one fixture UUID
// and want to distinguish the group containing it from the other schemes'
// groups.
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
		Handle: func(g ManifestEntryGroup) {
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
	w.scan(false)
	assert.Empty(t, handled, "first sight: buckets the group for retry")
	assert.NotEmpty(t, w.retries, "retry bucket must contain the held group")
	assert.NotContains(t, w.seen, uuid)

	w.scan(false)
	assert.Empty(t, handled, "retry scan 1: decrements 2->1, still held")

	w.scan(false)
	assert.Empty(t, handled, "retry scan 2: decrements 1->0, still held")

	w.scan(false)
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
		Handle: func(g ManifestEntryGroup) {
			if groupContainsUUID(g, uuid) {
				handled = append(handled, uuid)
			}
		},
		MatchProbe: func(ManifestEntryGroup) bool { return false },
		// MaxCorrelationRetries = 0 → opt out; fires immediately.
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false)

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
		Handle: func(g ManifestEntryGroup) {
			if groupContainsUUID(g, uuid) {
				handled = append(handled, uuid)
			}
		},
		MatchProbe:            nil,
		MaxCorrelationRetries: 6,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false)

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
		Handle: func(g ManifestEntryGroup) {
			handled = append(handled, g.UUIDs()...)
		},
		MatchProbe:            func(ManifestEntryGroup) bool { return false },
		MaxCorrelationRetries: 6,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(true)

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
		Handle:                func(ManifestEntryGroup) {},
		MatchProbe:            func(ManifestEntryGroup) bool { return false },
		MaxCorrelationRetries: 6,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false)

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
		Handle: func(g ManifestEntryGroup) {
			handled = append(handled, g.UUIDs()...)
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false)

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
		Handle: func(g ManifestEntryGroup) {
			handled = append(handled, g.UUIDs()...)
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false)

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
		Handle: func(g ManifestEntryGroup) {
			handled = append(handled, g.UUIDs()...)
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false)

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
		Handle: func(g ManifestEntryGroup) {
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
		Handle:       func(ManifestEntryGroup) {},
		HandledStore: store,
		Now:          func() time.Time { return fixtureNow },
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false)

	loaded, err := store.Load()
	require.NoError(t, err)

	var uuids []string
	for _, r := range loaded {
		uuids = append(uuids, r.UUID)
	}
	assert.Contains(t, uuids, uuid, "emitted UUID must be appended to HandledStore")
}

// writeMultiEntryManifest writes a manifest with multiple entries sharing the
// same scheme so GroupManifestEntries collapses them into a single group.
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

func TestWatcher_scan_ThreeEntriesSameScheme_EmitsOneGroup(t *testing.T) {
	home := t.TempDir()
	derivedRoot := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-grp")
	writeMultiEntryManifest(t, derivedRoot, "u1", "u2", "u3")

	var groups []ManifestEntryGroup
	w := &Watcher{
		Now:     func() time.Time { return fixtureNow },
		HomeDir: home,
		Handle: func(g ManifestEntryGroup) {
			groups = append(groups, g)
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false)

	require.Len(t, groups, 1, "same-scheme entries within TimeGap must emit as one group")
	assert.ElementsMatch(t, []string{"u1", "u2", "u3"}, groups[0].UUIDs())
	assert.Len(t, w.seen, 3, "every UUID in the emitted group is marked seen")
}

func TestWatcher_scan_AppendingUUIDToExistingGroup_EmitsOnceMore(t *testing.T) {
	home := t.TempDir()
	derivedRoot := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-append")
	writeMultiEntryManifest(t, derivedRoot, "u1", "u2")

	var handleCalls int
	var lastUUIDs []string
	w := &Watcher{
		Now:     func() time.Time { return fixtureNow },
		HomeDir: home,
		Handle: func(g ManifestEntryGroup) {
			handleCalls++
			lastUUIDs = g.UUIDs()
		},
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false)
	require.Equal(t, 1, handleCalls, "initial scan emits one group")
	assert.ElementsMatch(t, []string{"u1", "u2"}, lastUUIDs)

	// Rewrite the manifest with a new UUID added to the same group.
	writeMultiEntryManifest(t, derivedRoot, "u1", "u2", "u3")

	w.scan(false)
	require.Equal(t, 2, handleCalls, "appending a new UUID to the same scheme re-emits the group")
	assert.ElementsMatch(t, []string{"u1", "u2", "u3"}, lastUUIDs, "re-emitted group carries the enlarged UUID set (all entries, so upstream sees complete aggregation)")
}

func TestWatcher_scan_SkipsEntriesOlderThanHandledMaxAge(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)

	handles := 0

	w := &Watcher{
		HomeDir: home,
		Handle:  func(ManifestEntryGroup) { handles++ },
		Now:     func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) },
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false)

	assert.Zero(t, handles, "entries older than HandledManifestMaxAge must be skipped, not resurrected as orphans")
	assert.Empty(t, w.seen, "stale entries must not be marked seen either")
}
