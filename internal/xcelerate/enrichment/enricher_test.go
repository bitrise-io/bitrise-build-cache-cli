//go:build unit

package enrichment_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

func TestEnricher_MatchedPendingReusesID(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID: "kept-id",
		StartTime:    base,
		Duration:     10_000,
	}))

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{
		Store:            store,
		Client:           mock,
		XcodeVersion:     "16.2",
		XcodeBuildNumber: "16C5032a",
	}

	e.Enrich(enrichment.ManifestEntry{
		UUID:       "manifest-uuid",
		Signature:  "Build MyScheme",
		SchemeName: "MyScheme",
		Status:     "S",
		Start:      base.Add(2 * time.Second),
		Stop:       base.Add(8 * time.Second),
	})

	assert.Equal(t, "kept-id", captured.InvocationID)
	assert.Equal(t, "build MyScheme", captured.Command)
	assert.Equal(t, "Build MyScheme", captured.FullCommand)
	assert.True(t, captured.Success)
	assert.Equal(t, "16.2", captured.XcodeVersion)
	assert.Equal(t, "16C5032a", captured.ToolBuildNumber)

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
	e.Enrich(entry)

	assert.NotEmpty(t, captured.InvocationID)
	assert.False(t, captured.Success)
}

func TestEnricher_PutFailure_DoesNotRemovePending(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	base := time.Now()
	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID: "kept-id",
		StartTime:    base,
		Duration:     10_000,
	}))

	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error {
			return errors.New("boom")
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}
	e.Enrich(enrichment.ManifestEntry{
		Start: base.Add(1 * time.Second),
		Stop:  base.Add(2 * time.Second),
	})

	remaining, err := store.Load()
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, "kept-id", remaining[0].InvocationID)
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
		Auth: configcommon.CacheAuthConfig{
			WorkspaceID: "ws-1",
		},
		Metadata: configcommon.CacheConfigMetadata{
			BitriseAppID: "app-1",
		},
		Client: mock,
	}

	e.Enrich(enrichment.ManifestEntry{
		Signature: "Build S",
		Start:     time.Now(),
		Stop:      time.Now().Add(time.Second),
	})

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

	e.Enrich(enrichment.ManifestEntry{
		Signature: "Build S",
		Start:     now,
		Stop:      now.Add(time.Second),
	})

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

	e.Enrich(enrichment.ManifestEntry{
		Signature: "Build S",
		Start:     now,
		Stop:      now.Add(time.Second),
	})

	snap, err := enrichment.LoadHealth(hw.Path)
	require.NoError(t, err)
	assert.Equal(t, now, snap.LastAttempt.UTC())
	assert.True(t, snap.LastSuccess.IsZero())
	assert.Equal(t, 1, snap.ConsecutiveErrors)
	assert.Contains(t, snap.LastError, "network down")
}

func TestEnricher_PutFailure_RecordsAttempt(t *testing.T) {
	dir := t.TempDir()
	store := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	base := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)

	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID: "pre-existing",
		StartTime:    base,
		Duration:     10_000,
	}))

	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error { return errors.New("dial tcp: timeout") },
	}
	e := &enrichment.Enricher{
		Store:  store,
		Client: mock,
		Now:    func() time.Time { return base.Add(time.Minute) },
	}

	e.Enrich(enrichment.ManifestEntry{
		Signature: "Build S",
		Start:     base.Add(2 * time.Second),
		Stop:      base.Add(8 * time.Second),
	})

	loaded, err := store.Load()
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "pre-existing", loaded[0].InvocationID)
	assert.Equal(t, 1, loaded[0].Attempts)
	assert.Contains(t, loaded[0].LastError, "dial tcp")
	assert.NotEmpty(t, loaded[0].EnrichedPayload, "failed PUT must persist payload for retry")
	assert.Equal(t, base.Add(time.Minute), loaded[0].FirstAttempt.UTC())
	assert.Equal(t, base.Add(time.Minute), loaded[0].LastAttempt.UTC())
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

	e.Enrich(enrichment.ManifestEntry{
		UUID:      "orphan",
		Signature: "Archive S",
		Start:     now,
		Stop:      now.Add(time.Second),
	})

	loaded, err := store.Load()
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.NotEmpty(t, loaded[0].InvocationID)
	assert.Equal(t, 1, loaded[0].Attempts)
	assert.NotEmpty(t, loaded[0].EnrichedPayload)
	assert.Equal(t, now, loaded[0].FirstAttempt.UTC())
}

func TestEnricher_MatchedWithHandledMarker_SkipsPUT(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store := &enrichment.Store{Path: filepath.Join(home, "pending.ndjson")}

	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID: "kept-id",
		StartTime:    base,
		Duration:     10_000,
	}))

	// Wrapper wrote the marker for this invocation ID.
	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.XcelerateHandledInvocationDir(), 0o755))
	marker := p.XcelerateHandledInvocationFile("kept-id")
	require.NoError(t, os.WriteFile(marker, nil, 0o644))

	putCalls := 0
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(_ analytics.Invocation) error {
			putCalls++

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}
	e.Enrich(enrichment.ManifestEntry{
		Signature:  "Build MyScheme",
		SchemeName: "MyScheme",
		Status:     "S",
		Start:      base.Add(2 * time.Second),
		Stop:       base.Add(8 * time.Second),
	})

	assert.Zero(t, putCalls, "PUT must be skipped when the wrapper already handled the invocation")

	_, err := os.Stat(marker)
	require.NoError(t, err, "marker survives enrichment observation — startup sweep reclaims it")

	remaining, err := store.Load()
	require.NoError(t, err)
	assert.Empty(t, remaining, "pending record must be pruned once the wrapper is confirmed handler")
}

func TestEnricher_MatchedWithoutHandledMarker_PUTs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store := &enrichment.Store{Path: filepath.Join(home, "pending.ndjson")}

	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.Append(enrichment.PendingRecord{
		InvocationID: "kept-id",
		StartTime:    base,
		Duration:     10_000,
	}))

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}
	e.Enrich(enrichment.ManifestEntry{
		Signature:  "Build MyScheme",
		SchemeName: "MyScheme",
		Status:     "S",
		Start:      base.Add(2 * time.Second),
		Stop:       base.Add(8 * time.Second),
	})

	assert.Equal(t, "kept-id", captured.InvocationID, "matched pending without marker must PUT the enriched payload")
}

func TestEnricher_UnmatchedMintsAndPUTs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Stray marker for an unrelated ID — the orphan mint path uses a fresh UUID
	// so this marker must not accidentally block anything.
	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.XcelerateHandledInvocationDir(), 0o755))
	require.NoError(t, os.WriteFile(p.XcelerateHandledInvocationFile("stray"), nil, 0o644))

	store := &enrichment.Store{Path: filepath.Join(home, "pending.ndjson")}

	var captured analytics.Invocation
	mock := &InvocationPutterMock{
		PutInvocationFunc: func(inv analytics.Invocation) error {
			captured = inv

			return nil
		},
	}

	e := &enrichment.Enricher{Store: store, Client: mock}
	e.Enrich(enrichment.ManifestEntry{
		Signature: "Archive S",
		Start:     time.Now(),
		Stop:      time.Now().Add(time.Second),
	})

	assert.NotEmpty(t, captured.InvocationID)
	assert.NotEqual(t, "stray", captured.InvocationID, "orphan mint must not accidentally reuse an unrelated marker ID")
}
