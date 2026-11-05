//go:build unit

package enrichment_test

import (
	"bytes"
	"context"
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
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcactivitylog"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcresult"
)

// fakeXcresultParser returns a canned Summary per bundle path; absent paths
// fall through to the empty Summary.
type fakeXcresultParser struct {
	byPath map[string]xcresult.Summary
}

func (f *fakeXcresultParser) Parse(_ context.Context, bundlePath string) xcresult.Summary {
	if f == nil {
		return xcresult.Summary{}
	}

	return f.byPath[bundlePath]
}

func singleEntryGroup(e enrichment.ManifestEntry) enrichment.ManifestEntryGroup {
	return enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{e}}
}

// stageOKLog writes an xcactivitylog with a parseable CompilationCacheMetrics
// payload so the outcome-OK gate in Enrich() lets the test's PUT through.
// Returns (manifestPath, logFileName) to plug into the ManifestEntry.
func stageOKLog(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	manifestDir := filepath.Join(dir, "Logs", "Build")
	require.NoError(t, os.MkdirAll(manifestDir, 0o755))
	logName := "AAAA-BBBB.xcactivitylog"
	writeLog(t, manifestDir, logName, []byte("note: 7 hits / 10 cacheable tasks (70%)\n"))

	return filepath.Join(manifestDir, "LogStoreManifest.plist"), logName
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

	manifestPath, logName := stageOKLog(t)
	entry := enrichment.ManifestEntry{
		UUID:      "manifest-anchor-uuid",
		Signature: "Archive MyScheme",
		FileName:  logName,
		Status:    "E",
		Start:     time.Now(),
		Stop:      time.Now().Add(3 * time.Second),
	}
	e.Enrich(manifestPath, singleEntryGroup(entry))

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

	manifestPath, logName := stageOKLog(t)
	e.Enrich(manifestPath, singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "u-meta",
		Signature: "Build S",
		FileName:  logName,
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

	manifestPath, logName := stageOKLog(t)
	e.Enrich(manifestPath, singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "u-health-ok",
		Signature: "Build S",
		FileName:  logName,
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

	manifestPath, logName := stageOKLog(t)
	e.Enrich(manifestPath, singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "u-health-fail",
		Signature: "Build S",
		FileName:  logName,
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

	manifestPath, logName := stageOKLog(t)
	e.Enrich(manifestPath, singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "orphan",
		Signature: "Archive S",
		FileName:  logName,
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
	manifestPath, logName := stageOKLog(t)
	e.Enrich(manifestPath, singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "manifest-anchor-uuid",
		Signature: "Archive S",
		FileName:  logName,
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

	manifestPath, logName := stageOKLog(t)
	e.Enrich(manifestPath, singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "u-unmatched-success",
		Signature: "Archive S",
		FileName:  logName,
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
	manifestPath, logName := stageOKLog(t)
	e.Enrich(manifestPath, singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "u-orphan-duration",
		Signature: "Build S",
		FileName:  logName,
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

	manifestPath, logName := stageOKLog(t)
	group := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "u1", SchemeName: "S", Signature: "Build S", FileName: logName, Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		{UUID: "u2", SchemeName: "S", Signature: "Test S", FileName: logName, Status: "S", Start: base.Add(15 * time.Second), Stop: base.Add(45 * time.Second)},
	}}
	e.Enrich(manifestPath, group)

	assert.Equal(t, "build [S]", captured.Command,
		"Primary-only rendering: Build outranks Test as primary for the shared-scheme group")
	assert.Equal(t, "xcodebuild build -scheme S", captured.FullCommand,
		"Primary-only FullCommand surfaces the build verb; the test entry's duration is still folded into the span")
	assert.Equal(t, int64(45_000), captured.DurationMs,
		"duration = group.Duration() — the full min-Start → max-Stop span across all clustered actions")
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
	manifestPath, logName := stageOKLog(t)
	e.Enrich(manifestPath, singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "wrapperless-failed",
		Signature: "Build S",
		FileName:  logName,
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

func (f *fakeSidecarReader) Lookup(_ enrichment.ManifestEntryGroup, _ enrichment.LookupFilter) (*blobstats.Snapshot, []string, string, bool) {
	return f.blob, f.paths, f.invocationID, f.found
}

func (f *fakeSidecarReader) Has(_ enrichment.ManifestEntryGroup, _ enrichment.LookupFilter) bool {
	return false
}

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
		"manifest's xcactivitylog UUID is the authoritative BE identity; sidecar blob stats attach to this row via Lookup, and the hex-bridge later re-PUTs under the same ID (ID-match guard then skips the duplicate)")
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
	dir := t.TempDir()
	manifestDir := filepath.Join(dir, "Logs", "Build")
	require.NoError(t, os.MkdirAll(manifestDir, 0o755))

	logName := "AAAA-BBBB.xcactivitylog"
	writeLog(t, manifestDir, logName, []byte("note: 1 hits / 10 cacheable tasks (10%)\n"))

	captured := &analytics.Invocation{}
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			*captured = inv

			return nil
		},
	}

	snap := blobSnapshotWith(7, 3, 4)
	e := &enrichment.Enricher{
		Store:         &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")},
		Client:        mock,
		Logger:        log.NewLogger(log.WithOutput(&bytes.Buffer{})),
		SidecarReader: &fakeSidecarReader{found: true, blob: snap},
	}

	now := time.Now()
	e.Enrich(filepath.Join(manifestDir, "LogStoreManifest.plist"), singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "wrapperless",
		Signature: "Build MyScheme",
		FileName:  logName,
		Status:    "S",
		Start:     now,
		Stop:      now.Add(time.Second),
	}))

	assert.InDelta(t, float32(0.1), captured.HitRate, 0.001, "log-parsed hit rate must win when outcome is OK")
	require.NotNil(t, captured.CacheBlobStats)
	assert.Equal(t, int64(7), captured.CacheBlobStats.Download.OpCount)
	assert.Equal(t, int64(3), captured.CacheBlobStats.Download.MissCount)
	assert.Equal(t, int64(4), captured.CacheBlobStats.Upload.OpCount)
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
		OnEmit:        func(id string, _ string, _ enrichment.ManifestEntryGroup) { emitted = append(emitted, id) },
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
		"OnEmit must receive the InvocationID actually sent — manifest UUID wins so the hex-bridge re-PUT lands on the same row")
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
		OnEmit: func(id string, _ string, _ enrichment.ManifestEntryGroup) { emitted = append(emitted, id) },
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
		SidecarReader: &fakeSidecarReader{found: true, blob: blobSnapshotWith(1, 0, 0), invocationID: "test-sidecar-id"},
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

func TestEnricher_FallsBackToManifestUUID_WhenNoSidecar(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	// Manifest-sourced group, sidecar reader returns found=false and no invocationID.
	e := &enrichment.Enricher{
		Store:         store,
		Client:        mock,
		SidecarReader: &fakeSidecarReader{found: false},
	}

	now := time.Now()
	manifestPath, logName := stageOKLog(t)
	e.Enrich(manifestPath, singleEntryGroup(enrichment.ManifestEntry{
		UUID:      "manifest-uuid",
		Signature: "Build S",
		FileName:  logName,
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
	g := enrichment.SyntheticManifestGroup("sidecar-x", now, now.Add(time.Second), "Build S", "S", "override-id", enrichment.SyntheticClassification{})
	e.Enrich("", g)

	assert.Equal(t, "override-id", captured.InvocationID)
}

func TestEnricher_UsesSidecarInvocationID_OnSyntheticGroupWithoutOverride(t *testing.T) {
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
	g := enrichment.SyntheticManifestGroup("sidecar-x", now, now.Add(time.Second), "Build S", "S", "", enrichment.SyntheticClassification{})
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
	g := enrichment.SyntheticManifestGroup("sidecar-x", now, now.Add(time.Second), "Build S", "S", "", enrichment.SyntheticClassification{})
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
	manifestPath, logName := stageOKLog(t)
	g1 := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "aaa", SchemeName: "S", Signature: "Build S", FileName: logName, Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		{UUID: "bbb", SchemeName: "S", Signature: "Build S", FileName: logName, Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
	}}
	g2 := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "bbb", SchemeName: "S", Signature: "Build S", FileName: logName, Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		{UUID: "aaa", SchemeName: "S", Signature: "Build S", FileName: logName, Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
	}}

	e.Enrich(manifestPath, g1)
	e.Enrich(manifestPath, g2)

	require.Len(t, ids, 2)
	assert.Equal(t, ids[0], ids[1])
	assert.Equal(t, "aaa", ids[0])
}

// TestEnricher_UsesWideClusterSpanForDuration locks in the multi-action
// aggregation: durationMs must span the whole IDE interaction (e.g. ⌘⇧K + ⌘B),
// not the primary entry's own window. A late non-primary entry joining the 60s
// cluster is treated as part of the same user-visible wall clock.
func TestEnricher_UsesWideClusterSpanForDuration(t *testing.T) {
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

	manifestPath, logName := stageOKLog(t)
	group := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		// Primary: Build (highest known rank), 10s window.
		{UUID: "u-primary", SchemeName: "S", Signature: "Build S", FileName: logName, Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		// Late post-build entry (unknown command rank) joining the cluster, 50s window.
		{UUID: "u-late", SchemeName: "S", Signature: "Indexing S", Status: "S", Start: base.Add(20 * time.Second), Stop: base.Add(70 * time.Second)},
	}}
	e.Enrich(manifestPath, group)

	assert.Equal(t, int64(70_000), captured.DurationMs,
		"durationMs = group.Duration() — the wide min-Start → max-Stop span across every clustered entry")
}

// TestEnricher_AggregatesHitRateAcrossActions pins the clustered-action
// aggregation: a clean+build cluster whose Build log reports 7/10 and a second
// build log (same cluster) reports 3/10 lands on the BE row as 10/20 = 0.5.
func TestEnricher_AggregatesHitRateAcrossActions(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	base := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)

	dir := t.TempDir()
	manifestDir := filepath.Join(dir, "Logs", "Build")
	require.NoError(t, os.MkdirAll(manifestDir, 0o755))

	logA := "AAAA.xcactivitylog"
	logB := "BBBB.xcactivitylog"
	writeLog(t, manifestDir, logA, []byte("note: 7 hits / 10 cacheable tasks (70%)\n"))
	writeLog(t, manifestDir, logB, []byte("note: 3 hits / 10 cacheable tasks (30%)\n"))

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}

	group := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "u-a", SchemeName: "S", Signature: "Build S", FileName: logA, Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		{UUID: "u-b", SchemeName: "S", Signature: "Build S", FileName: logB, Status: "S", Start: base.Add(20 * time.Second), Stop: base.Add(30 * time.Second)},
	}}
	e.Enrich(filepath.Join(manifestDir, "LogStoreManifest.plist"), group)

	assert.InDelta(t, float32(0.5), captured.HitRate, 0.0001,
		"aggregate hit rate = Σhits/Σtotal across every parseable sibling log in the cluster (7+3)/(10+10)")
}

// TestEnricher_AggregatesHitRate_PartialOKFallsThroughOnlyOKEntries pins the
// "skip missing, keep OK" rule: when one entry's log is OK and another's is
// missing, the aggregate rate equals the OK entry's rate — the missing log
// does NOT demote the outcome to zero.
func TestEnricher_AggregatesHitRate_PartialOKFallsThroughOnlyOKEntries(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	base := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)

	dir := t.TempDir()
	manifestDir := filepath.Join(dir, "Logs", "Build")
	require.NoError(t, os.MkdirAll(manifestDir, 0o755))

	logA := "AAAA.xcactivitylog"
	writeLog(t, manifestDir, logA, []byte("note: 6 hits / 10 cacheable tasks (60%)\n"))

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}

	group := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "u-a", SchemeName: "S", Signature: "Build S", FileName: logA, Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		// Missing sibling log → ReadMetricsFromManifest returns FileMissing, must be skipped.
		{UUID: "u-b", SchemeName: "S", Signature: "Build S", FileName: "MISSING.xcactivitylog", Status: "S", Start: base.Add(20 * time.Second), Stop: base.Add(30 * time.Second)},
	}}
	e.Enrich(filepath.Join(manifestDir, "LogStoreManifest.plist"), group)

	require.Len(t, mock.PutInvocationCalls(), 1, "at least one OK log must let the PUT through")
	assert.InDelta(t, float32(0.6), captured.HitRate, 0.0001,
		"aggregate hit rate uses only the entry whose log parsed (6/10); the missing sibling is skipped, not counted as zero")
}

// TestEnricher_AggregatesXcresultSummaryAcrossActions pins the multi-bundle
// xcresult merge: a clean+build+test cluster writes three sibling xcresult
// bundles, and inv.Targets must carry the union deduped by Name (longest
// build duration wins when the same target appears across bundles).
func TestEnricher_AggregatesXcresultSummaryAcrossActions(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	base := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)

	dir := t.TempDir()
	manifestDir := filepath.Join(dir, "Logs", "Build")
	require.NoError(t, os.MkdirAll(manifestDir, 0o755))

	logBuild := "BUILD.xcactivitylog"
	logTest := "TEST.xcactivitylog"
	writeLog(t, manifestDir, logBuild, []byte("note: 0 hits / 1 cacheable tasks (0%)\n"))
	writeLog(t, manifestDir, logTest, []byte("note: 0 hits / 1 cacheable tasks (0%)\n"))

	bundleBuild := filepath.Join(manifestDir, "BUILD.xcresult")
	bundleTest := filepath.Join(manifestDir, "TEST.xcresult")
	require.NoError(t, os.MkdirAll(bundleBuild, 0o755))
	require.NoError(t, os.MkdirAll(bundleTest, 0o755))

	parser := &fakeXcresultParser{byPath: map[string]xcresult.Summary{
		bundleBuild: {
			Targets: []xcresult.TargetSummary{
				{Name: "App", BuildDurationMs: 1200},
				{Name: "Shared", BuildDurationMs: 300},
			},
			Failures: []xcresult.FailureSummary{{TargetName: "App", Message: "compile error"}},
		},
		bundleTest: {
			Targets: []xcresult.TargetSummary{
				{Name: "AppTests", BuildDurationMs: 500},
				// Same target appears in both bundles — longest duration wins.
				{Name: "Shared", BuildDurationMs: 800},
			},
			Failures: []xcresult.FailureSummary{
				// Duplicate failure must dedup.
				{TargetName: "App", Message: "compile error"},
				{TargetName: "AppTests", Message: "test failed"},
			},
		},
	}}

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock, XcresultParser: parser}

	group := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "u-build", SchemeName: "S", Signature: "Build S", FileName: logBuild, Status: "S", Start: base, Stop: base.Add(10 * time.Second)},
		{UUID: "u-test", SchemeName: "S", Signature: "Test S", FileName: logTest, Status: "S", Start: base.Add(20 * time.Second), Stop: base.Add(40 * time.Second)},
	}}
	e.Enrich(filepath.Join(manifestDir, "LogStoreManifest.plist"), group)

	targetByName := map[string]int64{}
	for _, t := range captured.Targets {
		targetByName[t.Name] = t.BuildDurationMs
	}
	assert.Equal(t, int64(1200), targetByName["App"])
	assert.Equal(t, int64(500), targetByName["AppTests"])
	assert.Equal(t, int64(800), targetByName["Shared"], "same-named target across bundles keeps the longest duration")
	assert.Len(t, captured.Targets, 3, "targets dedup by name across the merged bundles")

	require.Len(t, captured.Failures, 2, "failures dedup by (TargetName, Message) across the merged bundles")
	failureKeys := map[string]struct{}{}
	for _, f := range captured.Failures {
		failureKeys[f.TargetName+"|"+f.Message] = struct{}{}
	}
	assert.Contains(t, failureKeys, "App|compile error")
	assert.Contains(t, failureKeys, "AppTests|test failed")
}

// TestAggregateHitRate_UsesCacheWhenProvided pins the per-tick cache seam:
// when the Watcher installs a MetricsLookup via SetMetricsLookup, Enrich must
// route xcactivitylog reads through it and never call ReadMetricsFromManifest
// directly — so a log is gunzipped at most once per scan regardless of how
// many entries a cluster holds.
func TestEnricher_AggregateHitRate_UsesCacheWhenProvided(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}

	calls := map[string]int{}
	e.SetMetricsLookup(func(entry enrichment.ManifestEntry) xcactivitylog.Metrics {
		calls[entry.FileName]++

		return xcactivitylog.Metrics{
			Outcome: xcactivitylog.OutcomeOK,
			Hits:    5,
			Total:   10,
		}
	})

	base := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	group := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "u1", SchemeName: "S", Signature: "Build S", FileName: "A.xcactivitylog", Status: "S", Start: base, Stop: base.Add(5 * time.Second)},
	}}
	e.Enrich("/tmp/LogStoreManifest.plist", group)

	assert.InDelta(t, float32(0.5), captured.HitRate, 0.0001, "hit rate came from the stub lookup — never touched disk")
	assert.Equal(t, 1, calls["A.xcactivitylog"], "lookup must be called exactly once per entry")
}

// TestAggregateHitRate_FallsBackToReadWhenCacheNil pins the no-cache path:
// when no MetricsLookup is installed, Enrich continues to read via
// ReadMetricsFromManifest. Guards against a regression where the lookup is
// silently required.
func TestEnricher_AggregateHitRate_FallsBackToReadWhenCacheNil(t *testing.T) {
	store := &enrichment.Store{Path: filepath.Join(t.TempDir(), "pending.ndjson")}

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	// No SetMetricsLookup call: aggregateHitRate must still read directly.
	e := &enrichment.Enricher{Store: store, Client: mock}

	manifestPath, logName := stageOKLog(t)
	base := time.Now()
	group := enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{
		{UUID: "u", SchemeName: "S", Signature: "Build S", FileName: logName, Status: "S", Start: base, Stop: base.Add(time.Second)},
	}}
	e.Enrich(manifestPath, group)

	assert.InDelta(t, float32(0.7), captured.HitRate, 0.0001, "stageOKLog writes a 70% hit rate body; the direct-read path must still see it")
}
