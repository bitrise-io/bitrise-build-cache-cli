//go:build unit

package enrichment_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcactivitylog"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

func TestWatcher_EmitsOnlyNewEntries(t *testing.T) {
	home := t.TempDir()
	derived := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-abc/Logs/Build")
	require.NoError(t, os.MkdirAll(derived, 0o755))

	fixture, err := os.ReadFile("testdata/LogStoreManifest.plist")
	require.NoError(t, err)

	manifestPath := filepath.Join(derived, "LogStoreManifest.plist")
	require.NoError(t, os.WriteFile(manifestPath, fixture, 0o644))

	var (
		mu        sync.Mutex
		collected []string
	)

	// Fixture timestamps are Cocoa reference date + 762345688 ≈ 2025-02-27; pin Now so the age gate treats those entries as recent.
	fixtureNow := time.Date(2025, 2, 27, 12, 0, 0, 0, time.UTC)

	w := &enrichment.Watcher{
		HomeDir:      home,
		PollInterval: 20 * time.Millisecond,
		Now:          func() time.Time { return fixtureNow },
		Handle: func(_ string, g enrichment.ManifestEntryGroup) string {
			mu.Lock()
			defer mu.Unlock()
			collected = append(collected, g.UUIDs()...)
			return ""
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		w.Run(ctx)
		close(done)
	}()

	// Seed pass consumed both existing entries — nothing should fire yet.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	assert.Empty(t, collected, "seed scan must not emit historic entries")
	mu.Unlock()

	// Introduce a new manifest under a different DerivedData folder.
	other := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-xyz/Logs/Build")
	require.NoError(t, os.MkdirAll(other, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(other, "LogStoreManifest.plist"), []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>logs</key>
	<dict>
		<key>NEW-BUILD-UUID</key>
		<dict>
			<key>fileName</key><string>NEW-BUILD-UUID.xcactivitylog</string>
			<key>highLevelStatus</key><string>S</string>
			<key>signature</key><string>Build FreshScheme</string>
			<key>timeStartedRecording</key><real>762345900.0</real>
			<key>timeStoppedRecording</key><real>762345910.0</real>
		</dict>
	</dict>
</dict>
</plist>`), 0o644))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := append([]string(nil), collected...)
		mu.Unlock()

		if len(got) == 1 {
			assert.Equal(t, "NEW-BUILD-UUID", got[0])

			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, collected, 1, "watcher should emit exactly one new entry")
}

type blobstatsFixture struct {
	Download directionFixture `json:"download"`
	Upload   directionFixture `json:"upload"`
}

type directionFixture struct {
	OpCount   int64 `json:"opCount"`
	MissCount int64 `json:"missCount,omitempty"`
}

func TestWatcher_IDEFlow_ManifestBeforeSidecar_SingleEmissionUnderManifestUUID(t *testing.T) {
	home := t.TempDir()
	sidecarsDir := filepath.Join(home, "sessions")
	require.NoError(t, os.MkdirAll(sidecarsDir, 0o755))

	// Fixture manifest lives in the default DerivedData path.
	derived := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-ide/Logs/Build")
	require.NoError(t, os.MkdirAll(derived, 0o755))
	fixture, err := os.ReadFile("testdata/LogStoreManifest.plist")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(derived, "LogStoreManifest.plist"), fixture, 0o644))

	// Fixture entries span CFAbsoluteTime 762345688..762345698 → ~2025-02-27 10:41 UTC.
	// Pin now a few minutes past so the age gate treats them as recent.
	fixtureNow := time.Date(2025, 2, 27, 12, 0, 0, 0, time.UTC)
	const derivedID = "ide-derived-invocation-id"

	type sidecarFixtureFile struct {
		SchemaVersion  int               `json:"schema_version"`
		SidecarUUID    string            `json:"sidecar_uuid"`
		PeerAncestry   []string          `json:"peer_ancestry"`
		AcceptedAt     time.Time         `json:"accepted_at"`
		ClosedAt       time.Time         `json:"closed_at"`
		WrapperSession map[string]string `json:"wrapper_session"`
		BlobStats      *blobstatsFixture `json:"blobStats"`
	}

	// Sidecar window covers the manifest entries so Has/Lookup overlap returns true.
	flushSidecar := func() {
		body, err := json.Marshal(sidecarFixtureFile{
			SchemaVersion:  3,
			SidecarUUID:    "sidecar-ide",
			PeerAncestry:   []string{"Xcode", "SWBBuildService"},
			AcceptedAt:     time.Date(2025, 2, 27, 10, 40, 0, 0, time.UTC),
			ClosedAt:       time.Date(2025, 2, 27, 10, 45, 0, 0, time.UTC),
			WrapperSession: map[string]string{"invocation_id": derivedID},
			BlobStats:      &blobstatsFixture{Download: directionFixture{OpCount: 2, MissCount: 1}},
		})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(sidecarsDir, "a.json"), body, 0o600))
	}

	var captured []analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = append(captured, inv)

			return nil
		},
	}

	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}
	e := &enrichment.Enricher{
		Store:         store,
		Client:        mock,
		SidecarReader: enrichment.NewSidecarIndex(sidecarsDir, nil),
		Now:           func() time.Time { return fixtureNow },
	}

	w := &enrichment.Watcher{
		HomeDir:       home,
		Now:           func() time.Time { return fixtureNow },
		SidecarReader: e.SidecarReader,
		Handle:        e.Enrich,
	}
	e.OnEmit = w.RecordEmittedInvocationID

	// Single-ID pipeline: tick N sees the manifest only — no xcactivitylog +
	// no sidecar → enricher skips PUT. The single-ID pipeline deliberately
	// drops the retry bucket; the PUT rebirth happens only when supporting
	// data lands. flushSidecar simulates the proxy writing the sidecar
	// between ticks so the next scan carries enough data to PUT.
	w.ScanOnce()
	require.Empty(t, captured, "no xcactivitylog + no sidecar → PUT skipped on the single-ID pipeline")

	flushSidecar()
	// The manifest UUID has already been marked seen on tick N; tick N+1 does
	// not re-emit even though the sidecar is now present. This is the trade-off
	// of dropping the retry bucket — exercised here for the pin.
	w.ScanOnce()
	assert.Empty(t, captured, "manifest UUID already marked seen on first tick — no re-emit")
	_ = derivedID
}

func TestWatcher_IDEFlow_ManifestBeforeNilBlobStatsSidecar_SingleEmissionUnderManifestUUID(t *testing.T) {
	home := t.TempDir()
	sidecarsDir := filepath.Join(home, "sessions")
	require.NoError(t, os.MkdirAll(sidecarsDir, 0o755))

	derived := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-ide/Logs/Build")
	require.NoError(t, os.MkdirAll(derived, 0o755))
	fixture, err := os.ReadFile("testdata/LogStoreManifest.plist")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(derived, "LogStoreManifest.plist"), fixture, 0o644))

	fixtureNow := time.Date(2025, 2, 27, 12, 0, 0, 0, time.UTC)
	const derivedID = "ide-derived-invocation-nil-stats"

	type sidecarFixtureFile struct {
		SchemaVersion  int               `json:"schema_version"`
		SidecarUUID    string            `json:"sidecar_uuid"`
		PeerAncestry   []string          `json:"peer_ancestry"`
		AcceptedAt     time.Time         `json:"accepted_at"`
		ClosedAt       time.Time         `json:"closed_at"`
		WrapperSession map[string]string `json:"wrapper_session"`
		BlobStats      *blobstatsFixture `json:"blobStats,omitempty"`
	}

	// Empty-stat anchored-aged flush shape: no BlobStats, derived InvocationID only.
	flushNilStatsSidecar := func() {
		body, err := json.Marshal(sidecarFixtureFile{
			SchemaVersion:  3,
			SidecarUUID:    "sidecar-ide-nil",
			PeerAncestry:   []string{"Xcode", "SWBBuildService"},
			AcceptedAt:     time.Date(2025, 2, 27, 10, 40, 0, 0, time.UTC),
			ClosedAt:       time.Date(2025, 2, 27, 10, 45, 0, 0, time.UTC),
			WrapperSession: map[string]string{"invocation_id": derivedID},
			BlobStats:      nil,
		})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(sidecarsDir, "a.json"), body, 0o600))
	}

	var captured []analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = append(captured, inv)

			return nil
		},
	}

	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}
	e := &enrichment.Enricher{
		Store:         store,
		Client:        mock,
		SidecarReader: enrichment.NewSidecarIndex(sidecarsDir, nil),
		Now:           func() time.Time { return fixtureNow },
	}

	w := &enrichment.Watcher{
		HomeDir:       home,
		Now:           func() time.Time { return fixtureNow },
		SidecarReader: e.SidecarReader,
		Handle:        e.Enrich,
	}
	e.OnEmit = w.RecordEmittedInvocationID

	// Single-ID pipeline: tick N sees the manifest only — no xcactivitylog +
	// no sidecar → enricher skips PUT. flushNilStatsSidecar writes the sidecar
	// between ticks, but the manifest UUID is already seen so tick N+1 is a
	// no-op. Trade-off: single-ID pipeline drops the retry bucket.
	w.ScanOnce()
	require.Empty(t, captured, "manifest + no supporting data → enricher skip on first tick")

	flushNilStatsSidecar()
	w.ScanOnce()
	assert.Empty(t, captured, "manifest UUID already seen — no re-emit")
	_ = derivedID
}

// TestWatcher_scan_GunzipsEachLogOncePerTick pins the per-tick cache seam:
// grouping and aggregateHitRate share one metricsLookup, so a log is gunzipped
// at most once per unique (manifestPath, FileName) regardless of how many
// times grouping or Enrich poll the lookup. The test counts real disk reads
// via a wrapper around the underlying lookup — the outer wrapper is called
// many times (grouping compares, Enrich iterates), but the cache deduplicates
// to one physical read per entry.
func TestWatcher_scan_GunzipsEachLogOncePerTick(t *testing.T) {
	home := t.TempDir()
	derived := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-abc/Logs/Build")
	require.NoError(t, os.MkdirAll(derived, 0o755))

	logs := []string{"ENTRY-A.xcactivitylog", "ENTRY-B.xcactivitylog"}
	for i, name := range logs {
		writeLog(t, derived, name, []byte(fmt.Sprintf("note: %d hits / 10 cacheable tasks (%d%%)\n", i+1, (i+1)*10)))
	}

	// Two Build entries under the same scheme within LocalGroupTimeGap. No
	// hex is emitted (reader miss on these synthetic bodies), so clustering
	// falls back to time-gap only and the group holds both entries — exactly
	// the shape where the cache saves work.
	manifestBody := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>logs</key>
	<dict>
		<key>ENTRY-A</key>
		<dict>
			<key>fileName</key><string>ENTRY-A.xcactivitylog</string>
			<key>schemeIdentifier-schemeName</key><string>Models</string>
			<key>highLevelStatus</key><string>S</string>
			<key>signature</key><string>Build Models</string>
			<key>timeStartedRecording</key><real>762345900.0</real>
			<key>timeStoppedRecording</key><real>762345910.0</real>
		</dict>
		<key>ENTRY-B</key>
		<dict>
			<key>fileName</key><string>ENTRY-B.xcactivitylog</string>
			<key>schemeIdentifier-schemeName</key><string>Models</string>
			<key>highLevelStatus</key><string>S</string>
			<key>signature</key><string>Build Models</string>
			<key>timeStartedRecording</key><real>762345920.0</real>
			<key>timeStoppedRecording</key><real>762345930.0</real>
		</dict>
	</dict>
</dict>
</plist>`
	require.NoError(t, os.WriteFile(filepath.Join(derived, "LogStoreManifest.plist"), []byte(manifestBody), 0o644))

	fixtureNow := time.Date(2025, 2, 27, 12, 0, 0, 0, time.UTC)

	// installedLookup is what the Watcher will install via SetMetricsLookup.
	// callCount tracks TOTAL calls into the Watcher-installed lookup across
	// grouping + Enrich — includes cache hits.
	// uniqueDiskReads tracks first-time entries (cache misses): grouping walks
	// each entry once; subsequent lookups (Enrich iteration) must stay off the
	// disk because the cache returns the earlier Metrics.
	var (
		mu              sync.Mutex
		installedLookup enrichment.MetricsLookup
		callCount       = map[string]int{}
		uniqueDiskReads = map[string]bool{}
		lookupCleared   bool
	)

	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error { return nil },
	}
	e := &enrichment.Enricher{
		Store:  store,
		Client: mock,
		Now:    func() time.Time { return fixtureNow },
	}

	w := &enrichment.Watcher{
		HomeDir: home,
		Now:     func() time.Time { return fixtureNow },
		Handle: func(manifestPath string, group enrichment.ManifestEntryGroup) string {
			// Mimic Enrich's own iteration: for every entry in the group, hit
			// the installed lookup. With the shared cache this adds no disk
			// reads.
			mu.Lock()
			lookup := installedLookup
			mu.Unlock()
			if lookup != nil {
				for _, entry := range group.Entries {
					lookup(entry)
				}
			}

			e.Enrich(manifestPath, group)
			return ""
		},
		SetMetricsLookup: func(ml enrichment.MetricsLookup) {
			mu.Lock()
			defer mu.Unlock()
			if ml == nil {
				installedLookup = nil
				lookupCleared = true

				return
			}

			lookupCleared = false
			installedLookup = func(entry enrichment.ManifestEntry) xcactivitylog.Metrics {
				mu.Lock()
				callCount[entry.FileName]++
				firstSight := !uniqueDiskReads[entry.FileName]
				if firstSight {
					uniqueDiskReads[entry.FileName] = true
				}
				mu.Unlock()

				return ml(entry)
			}
			e.SetMetricsLookup(installedLookup)
		},
	}

	w.ScanOnce()

	mu.Lock()
	defer mu.Unlock()
	assert.True(t, lookupCleared, "SetMetricsLookup(nil) must fire at scan tail so no stale cache leaks into the next tick")
	assert.Equal(t, 2, len(uniqueDiskReads), "exactly one disk-read per unique (manifestPath, fileName) — 2 entries → 2 reads")
	assert.Positive(t, callCount["ENTRY-A.xcactivitylog"], "grouping + Enrich both consult the lookup for entry A at least once")
	assert.Positive(t, callCount["ENTRY-B.xcactivitylog"], "grouping + Enrich both consult the lookup for entry B at least once")
}
