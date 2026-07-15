//go:build unit

package enrichment_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

func TestStore_AppendLoadRemove(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	s := &enrichment.Store{
		Path: filepath.Join(dir, "pending.ndjson"),
		Now:  func() time.Time { return now },
	}

	rec := enrichment.PendingRecord{
		InvocationID: "inv-1",
		StartTime:    now.Add(-time.Minute),
		Duration:     42_000,
		HitRate:      0.75,
	}

	require.NoError(t, s.Append(rec))

	loaded, err := s.Load()
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, rec.InvocationID, loaded[0].InvocationID)
	assert.Equal(t, rec.Duration, loaded[0].Duration)
	assert.InDelta(t, rec.HitRate, loaded[0].HitRate, 1e-6)

	require.NoError(t, s.Remove("inv-1"))

	loaded, err = s.Load()
	require.NoError(t, err)
	assert.Empty(t, loaded)
}

func TestStore_Append_DoesNotEvictRetriedRecords(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	s := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	require.NoError(t, s.Append(enrichment.PendingRecord{
		InvocationID: "retried",
		StartTime:    now.Add(-3 * time.Hour),
		FirstAttempt: now.Add(-3 * time.Hour),
		Attempts:     3,
	}))
	require.NoError(t, s.Append(enrichment.PendingRecord{
		InvocationID: "fresh",
		StartTime:    now,
	}))

	loaded, err := s.Load()
	require.NoError(t, err)
	require.Len(t, loaded, 2, "Append must not evict retried records; eviction is Retrier.Sweep's job")
}

func TestStore_Load_MissingFile(t *testing.T) {
	dir := t.TempDir()
	s := &enrichment.Store{Path: filepath.Join(dir, "missing.ndjson")}

	loaded, err := s.Load()
	require.NoError(t, err)
	assert.Empty(t, loaded)
}

func TestStore_Load_SkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	s := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	require.NoError(t, s.Append(enrichment.PendingRecord{InvocationID: "good", StartTime: time.Now()}))

	// hand-crafted bad line appended after
	f, err := openAppend(s.Path)
	require.NoError(t, err)
	_, _ = f.WriteString("not-json\n")
	require.NoError(t, f.Close())

	loaded, err := s.Load()
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "good", loaded[0].InvocationID)
}

func TestStore_Remove_RecreatesMissingDir(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "does", "not", "exist", "pending.ndjson")
	s := &enrichment.Store{Path: nested}

	require.NoError(t, s.Remove("noop"), "Remove on missing dir/file must succeed")

	// Directory must be materialised by writeAtomic even though Load returned no records.
	_, err := os.Stat(filepath.Dir(nested))
	require.NoError(t, err, "writeAtomic must create the parent dir")
}

func TestStore_Save_ReplacesFile(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	s := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson"), Now: func() time.Time { return now }}

	require.NoError(t, s.Append(enrichment.PendingRecord{InvocationID: "a", StartTime: now}))
	require.NoError(t, s.Append(enrichment.PendingRecord{InvocationID: "b", StartTime: now}))

	require.NoError(t, s.Save([]enrichment.PendingRecord{{InvocationID: "only", StartTime: now}}))

	loaded, err := s.Load()
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "only", loaded[0].InvocationID)
}

func TestPendingRecord_ExtraFieldsRoundtrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	s := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson"), Now: func() time.Time { return now }}

	rec := enrichment.PendingRecord{
		InvocationID:    "with-retry",
		StartTime:       now,
		Duration:        1000,
		HitRate:         0.42,
		FirstAttempt:    now.Add(-time.Minute),
		LastAttempt:     now,
		Attempts:        3,
		LastError:       "network",
		EnrichedPayload: []byte(`{"foo":"bar"}`),
	}

	require.NoError(t, s.Append(rec))

	loaded, err := s.Load()
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	got := loaded[0]
	assert.Equal(t, rec.InvocationID, got.InvocationID)
	assert.Equal(t, rec.Attempts, got.Attempts)
	assert.Equal(t, rec.LastError, got.LastError)
	assert.JSONEq(t, string(rec.EnrichedPayload), string(got.EnrichedPayload))
	assert.Equal(t, rec.FirstAttempt.UTC(), got.FirstAttempt.UTC())
	assert.Equal(t, rec.LastAttempt.UTC(), got.LastAttempt.UTC())
}

func TestStore_ConcurrentAppends(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	s := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson"), Now: func() time.Time { return now }}

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			require.NoError(t, s.Append(enrichment.PendingRecord{
				InvocationID: fmt.Sprintf("inv-%d", i),
				StartTime:    now,
			}))
		}()
	}

	wg.Wait()

	loaded, err := s.Load()
	require.NoError(t, err)
	assert.Len(t, loaded, n)
}

func TestStore_Mutate_HoldsLockAcrossLoadAndSave(t *testing.T) {
	dir := t.TempDir()
	s := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	require.NoError(t, s.Append(enrichment.PendingRecord{InvocationID: "a"}))
	require.NoError(t, s.Append(enrichment.PendingRecord{InvocationID: "b"}))

	require.NoError(t, s.Mutate(func(existing []enrichment.PendingRecord) []enrichment.PendingRecord {
		out := existing[:0]
		for _, r := range existing {
			if r.InvocationID == "a" {
				continue
			}

			out = append(out, r)
		}

		return out
	}))

	loaded, err := s.Load()
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "b", loaded[0].InvocationID)
}

func TestStore_PruneOrphansOlderThan(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	s := &enrichment.Store{Path: filepath.Join(dir, "pending.ndjson")}

	// Old untouched orphan → prune.
	require.NoError(t, s.Append(enrichment.PendingRecord{
		InvocationID: "old-orphan",
		StartTime:    now.Add(-48 * time.Hour),
	}))
	// Retried record older than cutoff → keep (Retrier owns retry aging).
	require.NoError(t, s.Append(enrichment.PendingRecord{
		InvocationID: "old-retried",
		StartTime:    now.Add(-48 * time.Hour),
		FirstAttempt: now.Add(-48 * time.Hour),
		Attempts:     3,
	}))
	// Fresh untouched → keep.
	require.NoError(t, s.Append(enrichment.PendingRecord{
		InvocationID: "fresh",
		StartTime:    now.Add(-time.Hour),
	}))

	require.NoError(t, s.PruneOrphansOlderThan(now, 24*time.Hour))

	loaded, err := s.Load()
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	ids := []string{loaded[0].InvocationID, loaded[1].InvocationID}
	assert.Contains(t, ids, "old-retried")
	assert.Contains(t, ids, "fresh")
	assert.NotContains(t, ids, "old-orphan")
}
