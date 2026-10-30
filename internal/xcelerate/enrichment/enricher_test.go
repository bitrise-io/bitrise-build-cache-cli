//go:build unit

package enrichment_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

func singleEntryGroup(e enrichment.ManifestEntry) enrichment.ManifestEntryGroup {
	return enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{e}}
}

func TestEnricher_MatchedPendingSkipsPUTAndPrunesRecord(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID: "kept-id",
		StartTime:    base,
		Duration:     10_000,
	}))

	puts := 0
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error {
			puts++

			return nil
		},
	}

	e := &enrichment.Enricher{
		Store:            store,
		Client:           mock,
		XcodeVersion:     "16.2",
		XcodeBuildNumber: "16C5032a",
	}

	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		UUID:       "manifest-uuid",
		Signature:  "Build MyScheme",
		SchemeName: "MyScheme",
		Status:     "S",
		Start:      base.Add(2 * time.Second),
		Stop:       base.Add(8 * time.Second),
	}))

	assert.Zero(t, puts, "matched pending record must yield the InvocationID to the wrapper; re-PUT would clobber the rich row")

	remaining, err := store.Load()
	require.NoError(t, err)
	assert.Empty(t, remaining, "matched pending entry must be pruned")
}

func TestEnricher_NoMatchPutsUnderManifestUUID(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}

	entry := enrichment.ManifestEntry{
		UUID:      "manifest-anchor-uuid",
		Signature: "Archive MyScheme",
		Status:    "E",
		Start:     time.Now(),
		Stop:      time.Now().Add(3 * time.Second),
	}
	e.Enrich("", singleEntryGroup(entry))

	assert.Equal(t, "manifest-anchor-uuid", captured.InvocationID, "manifest UUID anchors the BE identity")
	assert.False(t, captured.Success)
}

func TestEnricher_CommandUnknown_SkipsPUT(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	base := time.Now()
	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID: "wrapper-id",
		StartTime:    base,
		Duration:     10_000,
	}))

	var puts int
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error {
			puts++

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}

	for _, sig := range []string{"Resolve Packages", "Update Signing", "Sync Localizations"} {
		e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
			UUID:      "side-effect-" + sig,
			Signature: sig,
			Status:    "S",
			Start:     base.Add(1 * time.Second),
			Stop:      base.Add(2 * time.Second),
		}))
	}

	assert.Zero(t, puts, "side-effect manifests must not trigger PutInvocation")

	loaded, err := store.Load()
	require.NoError(t, err)
	assert.Len(t, loaded, 1, "side-effect early-return must not consume time-overlapping pending records")
	assert.Equal(t, "wrapper-id", loaded[0].InvocationID)
}

func TestEnricher_MetadataForwarded(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{
		Store: store,
		Auth: auth.Credential{
			WorkspaceID: "ws-1",
		},
		Metadata: configcommon.CacheConfigMetadata{
			BitriseAppID: "app-1",
		},
		Client: mock,
	}

	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "u-meta",
		Signature: "Build S",
		Start:     time.Now(),
		Stop:      time.Now().Add(time.Second),
	}))

	assert.Equal(t, "ws-1", captured.BitriseOrgSlug)
	assert.Equal(t, "app-1", captured.BitriseAppSlug)
}

func TestEnricher_UpdatesHealth_OnSuccess(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}
	hw := &enrichment.HealthWriter{Path: filepath.Join(dir, "health.json")}

	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)

	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error { return nil },
	}
	e := &enrichment.Enricher{
		Store:  store,
		Client: mock,
		Health: hw,
		Now:    func() time.Time { return now },
	}

	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "u-health-ok",
		Signature: "Build S",
		Start:     now,
		Stop:      now.Add(time.Second),
	}))

	snap, err := enrichment.LoadHealth(hw.Path)
	require.NoError(t, err)
	assert.Equal(t, now, snap.LastAttempt.UTC())
	assert.Equal(t, now, snap.LastSuccess.UTC())
	assert.Zero(t, snap.ConsecutiveErrors)
	assert.Empty(t, snap.LastError)
}

func TestEnricher_UpdatesHealth_OnPutFailure(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}
	hw := &enrichment.HealthWriter{Path: filepath.Join(dir, "health.json")}

	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)

	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error { return errors.New("network down") },
	}
	e := &enrichment.Enricher{
		Store:  store,
		Client: mock,
		Health: hw,
		Now:    func() time.Time { return now },
	}

	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "u-health-fail",
		Signature: "Build S",
		Start:     now,
		Stop:      now.Add(time.Second),
	}))

	snap, err := enrichment.LoadHealth(hw.Path)
	require.NoError(t, err)
	assert.Equal(t, now, snap.LastAttempt.UTC())
	assert.True(t, snap.LastSuccess.IsZero())
	assert.Equal(t, 1, snap.ConsecutiveErrors)
	assert.Contains(t, snap.LastError, "network down")
}

func TestEnricher_PutFailure_OrphanCreatesFreshRecord(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)

	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error { return errors.New("boom") },
	}
	e := &enrichment.Enricher{
		Store:  store,
		Client: mock,
		Now:    func() time.Time { return now },
	}

	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "orphan",
		Signature: "Archive S",
		Start:     now,
		Stop:      now.Add(time.Second),
	}))

	loaded, err := store.Load()
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.NotEmpty(t, loaded[0].InvocationID)
	assert.Equal(t, 1, loaded[0].Attempts)
	assert.NotEmpty(t, loaded[0].EnrichedPayload)
	assert.Equal(t, now, loaded[0].FirstAttempt.UTC())
}

func TestEnricher_UnmatchedPutsUnderManifestUUID(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}
	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "manifest-anchor-uuid",
		Signature: "Archive S",
		Start:     time.Now(),
		Stop:      time.Now().Add(time.Second),
	}))

	assert.Equal(t, "manifest-anchor-uuid", captured.InvocationID,
		"unmatched (no pending) path still PUTs — now anchored by the manifest UUID, not a fresh UUID")
}

func TestEnricher_MatchedSkip_DoesNotTickHealth(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}
	hw := &enrichment.HealthWriter{Path: filepath.Join(dir, "health.json")}

	base := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID: "kept-id",
		StartTime:    base,
		Duration:     10_000,
	}))

	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error { return nil },
	}
	e := &enrichment.Enricher{
		Store:  store,
		Client: mock,
		Health: hw,
		Now:    func() time.Time { return base },
	}

	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		Signature: "Build S",
		Status:    "S",
		Start:     base.Add(2 * time.Second),
		Stop:      base.Add(8 * time.Second),
	}))

	snap, err := enrichment.LoadHealth(hw.Path)
	if err != nil {
		require.True(t, os.IsNotExist(err), "no health file must be written on the matched-skip path")

		return
	}
	assert.True(t, snap.LastAttempt.IsZero(), "matched skip must not tick LastAttempt")
	assert.True(t, snap.LastSuccess.IsZero(), "matched skip must not tick LastSuccess")
	assert.True(t, snap.LastMatched.IsZero(), "matched skip must not tick LastMatched")
}

func TestEnricher_UnmatchedSuccess_DoesNotTickLastMatched(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}
	hw := &enrichment.HealthWriter{Path: filepath.Join(dir, "health.json")}

	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error { return nil },
	}
	e := &enrichment.Enricher{
		Store:  store,
		Client: mock,
		Health: hw,
		Now:    func() time.Time { return now },
	}

	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "u-unmatched-success",
		Signature: "Archive S",
		Start:     now,
		Stop:      now.Add(time.Second),
	}))

	snap, err := enrichment.LoadHealth(hw.Path)
	require.NoError(t, err)
	assert.Equal(t, now, snap.LastSuccess.UTC(), "unmatched success still ticks LastSuccess")
	assert.True(t, snap.LastMatched.IsZero(),
		"unmatched (orphan) success must NOT bump LastMatched — that field is reserved for the correlated path")
}

func TestEnricher_OrphanDurationIsFromManifest(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	base := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}
	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "u-orphan-duration",
		Signature: "Build S",
		Status:    "S",
		Start:     base.Add(1 * time.Second),
		Stop:      base.Add(43 * time.Second),
	}))

	assert.Equal(t, int64(42_000), captured.DurationMs, "orphan-path duration comes from Stop-Start of the manifest entry")
}

func TestEnricher_MultiEntryGroup_AggregatesSpan(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}

	group := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "u1", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		{UUID: "u2", SchemeName: "S", Signature: "Test S", Status: "S", Start: base.Add(15 * time.Second), Stop: base.Add(45 * time.Second)},
	}}
	e.Enrich("", group)

	assert.Equal(t, "test [S]", captured.Command, "aggregate command uses the primary (test > build) plus scheme")
	assert.Equal(t, "Test S", captured.FullCommand, "aggregate FullCommand is the primary's signature")
	assert.Equal(t, int64(30_000), captured.DurationMs, "duration = PrimarySpan (Primary.Stop − Primary.Start), not the wide cluster span")
	assert.True(t, captured.Success)
	assert.Equal(t, base, captured.InvocationDate.UTC(), "aggregate InvocationDate = min(Start)")
}

func TestEnricher_WrapperlessFailurePopulatesSyntheticError(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}
	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "wrapperless-failed",
		Signature: "Build S",
		Status:    "E",
		Start:     time.Now(),
		Stop:      time.Now().Add(time.Second),
	}))

	assert.False(t, captured.Success)
	assert.Equal(t, "xcodebuild failed (status=E)", captured.Error,
		"wrapperless PUT must carry a synthetic Error so BE rows surface failure signal")
}

func TestEnricher_MultiEntryGroup_MixedSuccessAggregatesFalse(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}

	group := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "u1", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		{UUID: "u2", SchemeName: "S", Signature: "Test S", Status: "E", Start: base.Add(15 * time.Second), Stop: base.Add(45 * time.Second)},
	}}
	e.Enrich("", group)

	assert.False(t, captured.Success, "any failed entry fails the aggregate")
}

type fakeSidecarReader struct {
	blob         *blobstats.Snapshot
	paths        []string
	invocationID string
	found        bool
	orphans      map[string]enrichment.SidecarGroup
	ddPaths      []string
}

func (f *fakeSidecarReader) Lookup(_ enrichment.ManifestEntryGroup) (*blobstats.Snapshot, []string, string, bool) {
	return f.blob, f.paths, f.invocationID, f.found
}

func (f *fakeSidecarReader) Has(_ enrichment.ManifestEntryGroup) bool { return false }

func (f *fakeSidecarReader) OrphanSidecarsGroupedByInvocation(_ time.Time, _ time.Duration) map[string]enrichment.SidecarGroup {
	return f.orphans
}

func (f *fakeSidecarReader) PendingDerivedDataPaths() []string {
	return f.ddPaths
}

func (f *fakeSidecarReader) PendingCount() int {
	return len(f.ddPaths)
}

func blobSnapshotWith(downloadOps, downloadMiss, uploadOps int64) *blobstats.Snapshot {
	return &blobstats.Snapshot{
		Download: blobstats.DirectionSnapshot{OpCount: downloadOps, MissCount: downloadMiss},
		Upload:   blobstats.DirectionSnapshot{OpCount: uploadOps},
	}
}

func TestEnricher_SidecarPopulatesCacheBlobStats(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	snap := blobSnapshotWith(7, 3, 4)
	e := &enrichment.Enricher{
		Store:         store,
		Client:        mock,
		SidecarReader: &fakeSidecarReader{found: true, blob: snap},
	}

	now := time.Now()
	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "u-sidecar-blob",
		Signature: "Build S",
		Start:     now,
		Stop:      now.Add(time.Second),
	}))

	require.NotNil(t, captured.CacheBlobStats)
	assert.Equal(t, int64(7), captured.CacheBlobStats.Download.OpCount)
	assert.Equal(t, int64(3), captured.CacheBlobStats.Download.MissCount)
	assert.Equal(t, int64(4), captured.CacheBlobStats.Upload.OpCount)
}

func TestEnricher_ManifestUUIDWinsOverSidecarInvocationID(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	const sidecarID = "deadbeef-1111-4222-8333-444455556666"
	e := &enrichment.Enricher{
		Store:         store,
		Client:        mock,
		SidecarReader: &fakeSidecarReader{found: true, blob: blobSnapshotWith(1, 0, 0), invocationID: sidecarID},
	}

	now := time.Now()
	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "manifest-anchor",
		Signature: "Build S",
		Start:     now,
		Stop:      now.Add(time.Second),
	}))

	assert.Equal(t, "manifest-anchor", captured.InvocationID,
		"manifest UUID is the authoritative BE identity; sidecar InvocationID is a lookup-only fallback")
}

func TestEnricher_NoSidecarReaderLeavesCacheBlobStatsNil(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}

	now := time.Now()
	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "u-no-sidecar",
		Signature: "Build S",
		Start:     now,
		Stop:      now.Add(time.Second),
	}))

	assert.Nil(t, captured.CacheBlobStats)
}

func TestEnricher_SidecarDerivesHitRateWhenLogMissing(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{
		Store:         store,
		Client:        mock,
		SidecarReader: &fakeSidecarReader{found: true, blob: blobSnapshotWith(3, 1, 0)},
	}

	now := time.Now()
	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "u-sidecar-hitrate",
		Signature: "Build S",
		Start:     now,
		Stop:      now.Add(time.Second),
	}))

	assert.InDelta(t, 0.75, captured.HitRate, 0.0001)
}

func TestEnricher_SidecarPathsUnlinkedOnSuccessfulPut(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	p1 := filepath.Join(dir, "sidecar-a.json")
	p2 := filepath.Join(dir, "sidecar-b.json")
	require.NoError(t, os.WriteFile(p1, []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(p2, []byte("{}"), 0o600))

	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error { return nil },
	}

	e := &enrichment.Enricher{
		Store:  store,
		Client: mock,
		SidecarReader: &fakeSidecarReader{
			found: true,
			paths: []string{p1, p2},
		},
	}

	now := time.Now()
	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "u-unlink-success",
		Signature: "Build S",
		Start:     now,
		Stop:      now.Add(time.Second),
	}))

	_, err := os.Stat(p1)
	assert.True(t, os.IsNotExist(err), "p1 must be unlinked after a successful PUT")
	_, err = os.Stat(p2)
	assert.True(t, os.IsNotExist(err), "p2 must be unlinked after a successful PUT")
}

func TestEnricher_SidecarPathsUnlinkedOnPendingFailure(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	p1 := filepath.Join(dir, "sidecar-a.json")
	require.NoError(t, os.WriteFile(p1, []byte("{}"), 0o600))

	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error { return errors.New("boom") },
	}

	e := &enrichment.Enricher{
		Store:  store,
		Client: mock,
		SidecarReader: &fakeSidecarReader{
			found: true,
			paths: []string{p1},
		},
	}

	now := time.Now()
	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "u-unlink-pending",
		Signature: "Build S",
		Start:     now,
		Stop:      now.Add(time.Second),
	}))

	_, err := os.Stat(p1)
	assert.True(t, os.IsNotExist(err), "sidecar must be unlinked when the wrapperless record is persisted")

	loaded, err := store.Load()
	require.NoError(t, err)
	require.Len(t, loaded, 1, "pending retry record must be persisted")
}

func TestEnricher_SidecarDoesNotOverrideParsedLogHitRate(t *testing.T) {
	s := newEnrichSetup(t)
	writeLog(t, s.manifestDir, s.logName, []byte("note: 1 hits / 10 cacheable tasks (10%)\n"))

	snap := blobSnapshotWith(7, 3, 4)
	s.enricher.SidecarReader = &fakeSidecarReader{found: true, blob: snap}

	s.enricher.Enrich(s.manifestPath, s.group)

	assert.InDelta(t, float32(0.1), s.captured.HitRate, 0.001, "log-parsed hit rate must win when outcome is OK")
	require.NotNil(t, s.captured.CacheBlobStats)
	assert.Equal(t, int64(7), s.captured.CacheBlobStats.Download.OpCount)
	assert.Equal(t, int64(3), s.captured.CacheBlobStats.Download.MissCount)
	assert.Equal(t, int64(4), s.captured.CacheBlobStats.Upload.OpCount)
}

func TestEnricher_OnEmit_FiresWithPUTInvocationID(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error { return nil },
	}

	var emitted []string
	e := &enrichment.Enricher{
		Store:         store,
		Client:        mock,
		SidecarReader: &fakeSidecarReader{found: true, blob: blobSnapshotWith(1, 0, 0), invocationID: "sidecar-id"},
		OnEmit:        func(id string, _ enrichment.ManifestEntryGroup) { emitted = append(emitted, id) },
	}

	now := time.Now()
	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "manifest-anchor",
		Signature: "Build S",
		Start:     now,
		Stop:      now.Add(time.Second),
	}))

	require.Len(t, emitted, 1, "OnEmit must fire exactly once after a successful PUT")
	assert.Equal(t, "manifest-anchor", emitted[0],
		"OnEmit must receive the InvocationID actually sent — the manifest UUID anchors it, sidecar ID is ignored when manifest UUID is present")
}

func TestEnricher_OnEmit_DoesNotFireOnPutFailure(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error { return errors.New("boom") },
	}

	var emitted []string
	e := &enrichment.Enricher{
		Store:  store,
		Client: mock,
		OnEmit: func(id string, _ enrichment.ManifestEntryGroup) { emitted = append(emitted, id) },
	}

	now := time.Now()
	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "u-onemit-fail",
		Signature: "Build S",
		Start:     now,
		Stop:      now.Add(time.Second),
	}))

	assert.Empty(t, emitted, "OnEmit must not fire when PutInvocation failed")
}

func TestEnricher_EmptyGroup_NoOp(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	puts := 0
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error {
			puts++

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}
	e.Enrich("", enrichment.ManifestEntryGroup{})

	assert.Zero(t, puts)
}

func TestEnricher_PutsUnderManifestUUID_WhenManifestPresent(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{
		Store:         store,
		Client:        mock,
		SidecarReader: &fakeSidecarReader{found: true, blob: blobSnapshotWith(1, 0, 0), invocationID: "sidecar-id"},
	}

	now := time.Now()
	e.Enrich("", singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "manifest-uuid",
		Signature: "Build S",
		Start:     now,
		Stop:      now.Add(time.Second),
	}))

	assert.Equal(t, "manifest-uuid", captured.InvocationID)
}

func TestEnricher_FallsBackToOverride_WhenSyntheticGroup(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}

	now := time.Now()
	g := enrichment.SyntheticManifestGroup("sidecar-x", now, now.Add(time.Second), "Build S", "S", "override-id")
	e.Enrich("", g)

	assert.Equal(t, "override-id", captured.InvocationID)
}

func TestEnricher_FallsBackToSidecarLookup_WhenNoManifestNoOverride(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{
		Store:         store,
		Client:        mock,
		SidecarReader: &fakeSidecarReader{found: true, blob: blobSnapshotWith(1, 0, 0), invocationID: "sidecar-id"},
	}

	now := time.Now()
	g := enrichment.SyntheticManifestGroup("sidecar-x", now, now.Add(time.Second), "Build S", "S", "")
	e.Enrich("", g)

	assert.Equal(t, "sidecar-id", captured.InvocationID)
}

func TestEnricher_DropsAndWarns_WhenNoIdentitySource(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	puts := 0
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error {
			puts++

			return nil
		},
	}

	logBuf := &bytes.Buffer{}
	e := &enrichment.Enricher{
		Store:  store,
		Client: mock,
		Logger: log.NewLogger(log.WithOutput(logBuf)),
	}

	now := time.Now()
	g := enrichment.SyntheticManifestGroup("sidecar-x", now, now.Add(time.Second), "Build S", "S", "")
	e.Enrich("", g)

	assert.Zero(t, puts)
	assert.Contains(t, logBuf.String(), "no InvocationID source")
}

func TestEnricher_DeterministicReputs_ProduceSameInvocationID(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	var ids []string
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			ids = append(ids, inv.InvocationID)

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}

	base := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	g1 := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "aaa", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		{UUID: "bbb", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
	}}
	g2 := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "bbb", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		{UUID: "aaa", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
	}}

	e.Enrich("", g1)
	e.Enrich("", g2)

	require.Len(t, ids, 2)
	assert.Equal(t, ids[0], ids[1])
	assert.Equal(t, "aaa", ids[0])
}

// TestEnricher_UsesPrimarySpanForDuration_NotWideClusterSpan locks in the
// Option-C fix: wrapperless BE rows reflect the Primary build-action window,
// not the aggregate span that grows as late non-Primary entries (post-build
// indexing, preview compiles) join the 60s cluster.
func TestEnricher_UsesPrimarySpanForDuration_NotWideClusterSpan(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	base := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}

	group := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		// Primary: Build (highest known rank), 10s window.
		{UUID: "u-primary", SchemeName: "S", Signature: "Build S", Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		// Late post-build entry (unknown command rank) joining the cluster, 50s window.
		{UUID: "u-late", SchemeName: "S", Signature: "Indexing S", Status: "S", Start: base.Add(20 * time.Second), Stop: base.Add(70 * time.Second)},
	}}
	e.Enrich("", group)

	assert.Equal(t, int64(10_000), captured.DurationMs,
		"durationMs comes from PrimarySpan — not the 70s wide cluster span inflated by the late indexing entry")
}
