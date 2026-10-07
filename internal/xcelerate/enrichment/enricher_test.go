//go:build unit

package enrichment_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcresult"
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

func TestEnricher_NoMatchMintsFreshID(t *testing.T) {
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
		UUID:      "orphan",
		Signature: "Archive MyScheme",
		Status:    "E",
		Start:     time.Now(),
		Stop:      time.Now().Add(3 * time.Second),
	}
	e.Enrich("", singleEntryGroup(entry))

	assert.NotEmpty(t, captured.InvocationID)
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

func TestEnricher_UnmatchedMintsAndPUTs(t *testing.T) {
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
		Signature: "Archive S",
		Start:     time.Now(),
		Stop:      time.Now().Add(time.Second),
	}))

	assert.NotEmpty(t, captured.InvocationID, "orphan path must mint an ID and PUT")
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

	assert.Equal(t, "test S", captured.Command, "aggregate command uses the primary (test > build) plus scheme")
	assert.Equal(t, "Test S", captured.FullCommand, "aggregate FullCommand is the primary's signature")
	assert.Equal(t, int64(45_000), captured.DurationMs, "aggregate duration = max(Stop) − min(Start)")
	assert.True(t, captured.Success)
	assert.Equal(t, base, captured.InvocationDate.UTC(), "aggregate InvocationDate = min(Start)")
}

func TestEnricher_OrphanFailurePopulatesSyntheticError(t *testing.T) {
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
		UUID:      "orphan-failed",
		Signature: "Build S",
		Status:    "E",
		Start:     time.Now(),
		Stop:      time.Now().Add(time.Second),
	}))

	assert.False(t, captured.Success)
	assert.Equal(t, "xcodebuild failed (status=E)", captured.Error,
		"orphan PUT must carry a synthetic Error so BE rows surface failure signal")
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
	blob  *blobstats.Snapshot
	paths []string
	found bool
}

func (f *fakeSidecarReader) Lookup(_ enrichment.ManifestEntryGroup) (*blobstats.Snapshot, []string, bool) {
	return f.blob, f.paths, f.found
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
		Signature: "Build S",
		Start:     now,
		Stop:      now.Add(time.Second),
	}))

	require.NotNil(t, captured.CacheBlobStats)
	assert.Equal(t, int64(7), captured.CacheBlobStats.Download.OpCount)
	assert.Equal(t, int64(3), captured.CacheBlobStats.Download.MissCount)
	assert.Equal(t, int64(4), captured.CacheBlobStats.Upload.OpCount)
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
		Signature: "Build S",
		Start:     now,
		Stop:      now.Add(time.Second),
	}))

	_, err := os.Stat(p1)
	assert.True(t, os.IsNotExist(err), "sidecar must be unlinked when the orphan record is persisted")

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

type fakeXcresultParser struct {
	summary xcresult.Summary
	calls   atomic.Int32
}

func (p *fakeXcresultParser) Parse(_ context.Context, _ string) xcresult.Summary {
	p.calls.Add(1)

	return p.summary
}

func TestEnricher_OrphanAttachesXcresultSummary(t *testing.T) {
	s := newEnrichSetup(t)

	bundlePath := filepath.Join(s.manifestDir, "AAAA-BBBB.xcresult")
	require.NoError(t, os.Mkdir(bundlePath, 0o755))

	parser := &fakeXcresultParser{summary: xcresult.Summary{
		Targets: []xcresult.TargetSummary{
			{Name: "TargetA", BuildDurationMs: 1234},
			{Name: "TargetB", BuildDurationMs: 5678},
		},
		Failures: []xcresult.FailureSummary{
			{TargetName: "TargetA", Message: "compile error"},
		},
	}}
	s.enricher.XcresultParser = parser

	s.enricher.Enrich(s.manifestPath, s.group)

	require.Equal(t, int32(1), parser.calls.Load())
	require.Len(t, s.captured.Targets, 2)
	assert.Equal(t, "TargetA", s.captured.Targets[0].Name)
	assert.Equal(t, int64(1234), s.captured.Targets[0].BuildDurationMs)
	assert.Equal(t, "TargetB", s.captured.Targets[1].Name)
	require.Len(t, s.captured.Failures, 1)
	assert.Equal(t, "TargetA", s.captured.Failures[0].TargetName)
	assert.Equal(t, "compile error", s.captured.Failures[0].Message)
}

func TestEnricher_OrphanSkipsXcresultWhenBundleMissing(t *testing.T) {
	s := newEnrichSetup(t)

	parser := &fakeXcresultParser{summary: xcresult.Summary{
		Targets: []xcresult.TargetSummary{{Name: "TargetA"}},
	}}
	s.enricher.XcresultParser = parser

	s.enricher.Enrich(s.manifestPath, s.group)

	assert.Zero(t, parser.calls.Load(), "parser must not be invoked when the bundle is absent")
	assert.Empty(t, s.captured.Targets)
	assert.Empty(t, s.captured.Failures)
}

func TestEnricher_OrphanSkipsXcresultWhenParserNil(t *testing.T) {
	s := newEnrichSetup(t)

	bundlePath := filepath.Join(s.manifestDir, "AAAA-BBBB.xcresult")
	require.NoError(t, os.Mkdir(bundlePath, 0o755))

	s.enricher.Enrich(s.manifestPath, s.group)

	assert.Empty(t, s.captured.Targets)
	assert.Empty(t, s.captured.Failures)
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
