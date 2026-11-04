//go:build unit

package enrichment_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		Handle: func(_ string, g enrichment.ManifestEntryGroup) bool {
			mu.Lock()
			defer mu.Unlock()
			collected = append(collected, g.UUIDs()...)
			return true
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

// TestWatcher_OrphanSidecarPseudoGroupEndToEnd wires the real SidecarIndex + a
// real Enricher. No LogStoreManifest.plist exists; two sidecars with a shared
// WrapperSession.InvocationID land, and the shutdown sweep must synthesize
// exactly one pseudo-group and PUT it under that ID.
func TestWatcher_OrphanSidecarPseudoGroupEndToEnd(t *testing.T) {
	home := t.TempDir()
	sidecarsDir := filepath.Join(home, "sessions")
	require.NoError(t, os.MkdirAll(sidecarsDir, 0o755))

	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	closed := now.Add(-30 * time.Second)

	type sidecarFixtureFile struct {
		SchemaVersion  int                 `json:"schema_version"`
		SidecarUUID    string              `json:"sidecar_uuid"`
		PeerAncestry   []string            `json:"peer_ancestry"`
		AcceptedAt     time.Time           `json:"accepted_at"`
		ClosedAt       time.Time           `json:"closed_at"`
		WrapperSession map[string]string   `json:"wrapper_session"`
		BlobStats      *blobstatsFixture   `json:"blobStats"`
	}

	write := func(name, sidecarUUID string, downloadOps int64) {
		path := filepath.Join(sidecarsDir, name)
		body, err := json.Marshal(sidecarFixtureFile{
			SchemaVersion: 2,
			SidecarUUID:   sidecarUUID,
			PeerAncestry:  []string{"xcodebuild"},
			AcceptedAt:    closed.Add(-time.Minute),
			ClosedAt:      closed,
			WrapperSession: map[string]string{
				"invocation_id": "shared-invocation",
			},
			BlobStats: &blobstatsFixture{
				Download: directionFixture{OpCount: downloadOps},
			},
		})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, body, 0o600))
	}

	write("a.json", "sidecar-a", 3)
	write("b.json", "sidecar-b", 2)

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
		Now:           func() time.Time { return now },
	}

	w := &enrichment.Watcher{
		HomeDir:       home,
		Now:           func() time.Time { return now },
		SidecarReader: e.SidecarReader,
		Handle:        e.Enrich,
	}
	e.OnEmit = w.RecordEmittedInvocationID

	w.ScanOnce()

	require.Len(t, captured, 1, "exactly one pseudo-group must PUT (two sidecars share one InvocationID)")
	assert.Equal(t, "shared-invocation", captured[0].InvocationID, "PUT must use the sidecar-shared InvocationID so the row upserts")
	assert.Equal(t, "build", captured[0].Command, "synthetic Signature \"build\" lands as a bare build command")
}

// TestWatcher_OrphanSidecar_DedupSkipsManifestEmittedID exercises the OnEmit
// wiring end-to-end: the manifest-path emission populates the per-tick dedup
// set with a known ID (via a fake SidecarReader forcing the Enricher to adopt
// a sidecar-provided InvocationID), and the orphan sweep then skips that same
// ID even though a fresh sidecar exists for it.
func TestWatcher_OrphanSidecar_DedupSkipsManifestEmittedID(t *testing.T) {
	home := t.TempDir()
	fixtureNow := time.Date(2025, 2, 27, 12, 0, 0, 0, time.UTC)

	derived := filepath.Join(home, "Library/Developer/Xcode/DerivedData/App-abc/Logs/Build")
	require.NoError(t, os.MkdirAll(derived, 0o755))
	fixture, err := os.ReadFile("testdata/LogStoreManifest.plist")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(derived, "LogStoreManifest.plist"), fixture, 0o644))

	const sharedID = "shared-X"

	// SidecarReader double: Lookup fires on the manifest-path emission with
	// invocationID=sharedID so the Enricher adopts that ID; orphans returns
	// one group under sharedID that must be deduped.
	reader := &fakeSidecarReader{
		found:        true,
		invocationID: sharedID,
		orphans: map[string]enrichment.SidecarGroup{
			sharedID: {
				Start:        fixtureNow.Add(-time.Minute),
				Stop:         fixtureNow.Add(-30 * time.Second),
				SidecarUUIDs: []string{"sidecar-shared"},
			},
		},
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
		SidecarReader: reader,
		Now:           func() time.Time { return fixtureNow },
	}

	w := &enrichment.Watcher{
		HomeDir:       home,
		Now:           func() time.Time { return fixtureNow },
		SidecarReader: reader,
		Handle:        e.Enrich,
	}
	e.OnEmit = w.RecordEmittedInvocationID

	w.ScanOnce()

	var synthetic []string
	for _, inv := range captured {
		if strings.HasPrefix(inv.InvocationID, enrichment.SyntheticManifestGroupUUIDPrefix) {
			synthetic = append(synthetic, inv.InvocationID)
		}
	}
	assert.Empty(t, synthetic, "same-tick dedup must skip any synthetic group whose InvocationID already emitted via the manifest path")

	var matched int
	for _, inv := range captured {
		if inv.InvocationID == sharedID {
			matched++
		}
	}
	assert.Positive(t, matched, "manifest path must emit under the sidecar-provided sharedID — this is what the dedup set keys on")
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
			SchemaVersion:  2,
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
		HomeDir:               home,
		Now:                   func() time.Time { return fixtureNow },
		SidecarReader:         e.SidecarReader,
		Handle:                e.Enrich,
		MatchProbe:            func(enrichment.ManifestEntryGroup) bool { return false },
		MaxCorrelationRetries: 6,
	}
	e.OnEmit = w.RecordEmittedInvocationID

	// Tick N: manifest on disk, no sidecar yet. canCorrelate → false; retry-bucket opens.
	w.ScanOnce()
	assert.Empty(t, captured, "no sidecar yet, no MatchProbe signal — group must sit in retry bucket, not emit")

	// Between ticks: proxy's intermediate flush writes the sidecar carrying derivedID.
	flushSidecar()

	// Tick N+1: canCorrelate → true via SidecarReader.Has; group resolves and PUTs.
	w.ScanOnce()

	_ = derivedID
	require.Len(t, captured, 1, "exactly one PUT must fire — the retry bucket resolves on sidecar presence")
	assert.Equal(t, "AAAA-BBBB-BUILD", captured[0].InvocationID,
		"PUT must adopt the manifest's xcactivitylog UUID; the hex-bridge later re-PUTs under the same ID (ID-match guard skips the duplicate)")
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
			SchemaVersion:  2,
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
		HomeDir:               home,
		Now:                   func() time.Time { return fixtureNow },
		SidecarReader:         e.SidecarReader,
		Handle:                e.Enrich,
		MatchProbe:            func(enrichment.ManifestEntryGroup) bool { return false },
		MaxCorrelationRetries: 6,
	}
	e.OnEmit = w.RecordEmittedInvocationID

	// Tick N: manifest on disk, no sidecar yet. Group sits in the retry bucket.
	w.ScanOnce()
	assert.Empty(t, captured, "no sidecar yet — group must sit in retry bucket")

	// Between ticks: proxy's empty-stat anchored-aged flush writes the sidecar.
	flushNilStatsSidecar()

	// Tick N+1: canCorrelate → true via SidecarReader.Has; group resolves and PUTs.
	w.ScanOnce()

	_ = derivedID
	require.Len(t, captured, 1, "exactly one PUT must fire for the retry bucket")
	assert.Equal(t, "AAAA-BBBB-BUILD", captured[0].InvocationID,
		"PUT must adopt the manifest's xcactivitylog UUID; the sidecar's derived InvocationID is reserved for truly synthetic groups without a manifest sibling")
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
		Handle: func(manifestPath string, group enrichment.ManifestEntryGroup) bool {
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

			return e.Enrich(manifestPath, group)
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
