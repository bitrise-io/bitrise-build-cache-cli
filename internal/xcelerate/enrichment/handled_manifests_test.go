//go:build unit

package enrichment_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

func TestHandledManifestStore_LoadEmptyMissingFile(t *testing.T) {
	dir := t.TempDir()
	s := &enrichment.HandledManifestStore{Path: filepath.Join(dir, "missing.ndjson")}

	got, err := s.Load()
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestHandledManifestStore_AppendLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s := &enrichment.HandledManifestStore{Path: filepath.Join(dir, "handled.ndjson")}
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)

	require.NoError(t, s.Append(enrichment.HandledManifest{UUID: "uuid-1", HandledAt: now}))
	require.NoError(t, s.Append(enrichment.HandledManifest{UUID: "uuid-2", HandledAt: now.Add(time.Minute)}))

	got, err := s.Load()
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "uuid-1", got[0].UUID)
	assert.Equal(t, "uuid-2", got[1].UUID)
	assert.Equal(t, now.UTC(), got[0].HandledAt.UTC())
}

func TestHandledManifestStore_LoadSkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "handled.ndjson")
	s := &enrichment.HandledManifestStore{Path: path}

	require.NoError(t, s.Append(enrichment.HandledManifest{UUID: "good", HandledAt: time.Now()}))

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, _ = f.WriteString("not-json\n")
	require.NoError(t, f.Close())

	got, err := s.Load()
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "good", got[0].UUID)
}

func TestHandledManifestStore_PruneOlderThan(t *testing.T) {
	dir := t.TempDir()
	s := &enrichment.HandledManifestStore{Path: filepath.Join(dir, "handled.ndjson")}
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)

	require.NoError(t, s.Append(enrichment.HandledManifest{UUID: "stale", HandledAt: now.Add(-10 * 24 * time.Hour)}))
	require.NoError(t, s.Append(enrichment.HandledManifest{UUID: "fresh", HandledAt: now.Add(-time.Hour)}))

	require.NoError(t, s.PruneOlderThan(now, enrichment.HandledManifestMaxAge))

	got, err := s.Load()
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "fresh", got[0].UUID)
}

func TestHandledManifestStore_PruneNoOpWhenNothingStale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "handled.ndjson")
	s := &enrichment.HandledManifestStore{Path: path}
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)

	require.NoError(t, s.Append(enrichment.HandledManifest{UUID: "fresh-1", HandledAt: now.Add(-time.Hour)}))
	require.NoError(t, s.Append(enrichment.HandledManifest{UUID: "fresh-2", HandledAt: now.Add(-2 * time.Hour)}))

	before, err := os.Stat(path)
	require.NoError(t, err)
	sizeBefore := before.Size()

	require.NoError(t, s.PruneOlderThan(now, enrichment.HandledManifestMaxAge))

	after, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, sizeBefore, after.Size(), "prune no-op must not rewrite the file")

	got, err := s.Load()
	require.NoError(t, err)
	assert.Len(t, got, 2)
}
