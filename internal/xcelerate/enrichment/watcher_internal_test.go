//go:build unit

package enrichment

import (
	"os"
	"path/filepath"
	"testing"

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
	w := &Watcher{
		HomeDir:               home,
		Handle:                func(ManifestEntry) { calls++ },
		MatchProbe:            func(ManifestEntry) bool { return true },
		MaxCorrelationRetries: 3,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false)

	// All manifest entries match, so all fire on the first scan.
	assert.Positive(t, calls)
	assert.Empty(t, w.retries)
	assert.Len(t, w.seen, calls)
}

func TestWatcher_scan_UnmatchedEntry_HeldForRetries(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)
	uuid := pickUUID(t, home)

	var scans int
	var handled []string
	w := &Watcher{
		HomeDir: home,
		Handle: func(e ManifestEntry) {
			if e.UUID == uuid {
				handled = append(handled, e.UUID)
			}
		},
		MatchProbe: func(e ManifestEntry) bool {
			if e.UUID != uuid {
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
	assert.NotContains(t, w.retries, uuid)
}

func TestWatcher_scan_UnmatchedEntry_MintedAsOrphanAfterMaxRetries(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)
	uuid := pickUUID(t, home)

	var handled []string
	w := &Watcher{
		HomeDir: home,
		Handle: func(e ManifestEntry) {
			if e.UUID == uuid {
				handled = append(handled, e.UUID)
			}
		},
		MatchProbe: func(e ManifestEntry) bool {
			return e.UUID != uuid
		},
		MaxCorrelationRetries: 2,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false)
	assert.Empty(t, handled, "first sight: buckets the uuid for retry")
	assert.Contains(t, w.retries, uuid)
	assert.NotContains(t, w.seen, uuid)

	w.scan(false)
	assert.Empty(t, handled, "second scan: decrements, still held")

	w.scan(false)
	assert.Equal(t, []string{uuid}, handled, "third scan: exhausted, mints as orphan")
	assert.Contains(t, w.seen, uuid)
	assert.NotContains(t, w.retries, uuid)
}

func TestWatcher_scan_ZeroRetries_MintsImmediately(t *testing.T) {
	home := t.TempDir()
	writeFixtureManifest(t, home)
	uuid := pickUUID(t, home)

	var handled []string
	w := &Watcher{
		HomeDir: home,
		Handle: func(e ManifestEntry) {
			if e.UUID == uuid {
				handled = append(handled, e.UUID)
			}
		},
		MatchProbe: func(ManifestEntry) bool { return false },
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
		HomeDir: home,
		Handle: func(e ManifestEntry) {
			if e.UUID == uuid {
				handled = append(handled, e.UUID)
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
		HomeDir: home,
		Handle: func(e ManifestEntry) {
			handled = append(handled, e.UUID)
		},
		MatchProbe:            func(ManifestEntry) bool { return false },
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
		HomeDir:               home,
		Handle:                func(ManifestEntry) {},
		MatchProbe:            func(ManifestEntry) bool { return false },
		MaxCorrelationRetries: 6,
	}
	w.seen = map[string]struct{}{}
	w.retries = map[string]int{}

	w.scan(false)

	assert.Contains(t, w.retries, uuid, "uuid must be in retries after first unmatched scan")
	assert.NotContains(t, w.seen, uuid, "unmatched uuid must NOT be in seen (otherwise second scan is skipped)")
}
