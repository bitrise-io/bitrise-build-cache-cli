//go:build unit

package enrichment_test

import (
	"path/filepath"
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

func TestStore_Append_PrunesOldEntries(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	s := &enrichment.Store{
		Path: filepath.Join(dir, "pending.ndjson"),
		Now:  func() time.Time { return now },
	}

	require.NoError(t, s.Append(enrichment.PendingRecord{
		InvocationID: "old",
		StartTime:    now.Add(-2 * time.Hour),
	}))
	require.NoError(t, s.Append(enrichment.PendingRecord{
		InvocationID: "fresh",
		StartTime:    now.Add(-5 * time.Minute),
	}))

	loaded, err := s.Load()
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "fresh", loaded[0].InvocationID)
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
