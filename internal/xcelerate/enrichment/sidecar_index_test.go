//go:build unit

package enrichment_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

type sidecarFixture struct {
	SchemaVersion int        `json:"schema_version"`
	PeerAncestry  []string   `json:"peer_ancestry"`
	AcceptedAt    time.Time  `json:"accepted_at"`
	ClosedAt      time.Time  `json:"closed_at"`
	Stats         fixtureSts `json:"stats"`
}

type fixtureSts struct {
	Hits          int64 `json:"hits"`
	Misses        int64 `json:"misses"`
	KVHits        int64 `json:"kv_hits"`
	KVMisses      int64 `json:"kv_misses"`
	Uploads       int64 `json:"uploads"`
	UploadBytes   int64 `json:"upload_bytes"`
	DownloadBytes int64 `json:"download_bytes"`
	KVUploadBytes int64 `json:"kv_upload_bytes"`
}

func writeSidecar(t *testing.T, dir, name string, s sidecarFixture) string {
	t.Helper()
	path := filepath.Join(dir, name)
	body, err := json.Marshal(s)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, body, 0o600))

	return path
}

func groupFor(start, stop time.Time) enrichment.ManifestEntryGroup {
	return enrichment.ManifestEntryGroup{Entries: []enrichment.ManifestEntry{{
		Signature: "Build S",
		Start:     start,
		Stop:      stop,
	}}}
}

func TestSidecarIndex_EmptyDirReturnsNotFound(t *testing.T) {
	dir := t.TempDir()
	idx := enrichment.NewSidecarIndex(dir, nil)

	stats, paths, ok := idx.Lookup(groupFor(time.Now(), time.Now().Add(time.Second)))
	assert.False(t, ok)
	assert.Empty(t, paths)
	assert.Zero(t, stats.Hits)
}

func TestSidecarIndex_MissingDirReturnsNotFound(t *testing.T) {
	idx := enrichment.NewSidecarIndex(filepath.Join(t.TempDir(), "nope"), nil)

	_, _, ok := idx.Lookup(groupFor(time.Now(), time.Now().Add(time.Second)))
	assert.False(t, ok)
}

func TestSidecarIndex_EmptyDirStringReturnsNotFound(t *testing.T) {
	idx := enrichment.NewSidecarIndex("", nil)

	_, _, ok := idx.Lookup(groupFor(time.Now(), time.Now().Add(time.Second)))
	assert.False(t, ok)
}

func TestSidecarIndex_AncestryRejectedWhenXcodebuildAbsent(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	writeSidecar(t, dir, "a.json", sidecarFixture{
		SchemaVersion: 1,
		PeerAncestry:  []string{"swift-driver", "clang"},
		AcceptedAt:    base,
		ClosedAt:      base.Add(10 * time.Second),
		Stats:         fixtureSts{Hits: 7},
	})

	idx := enrichment.NewSidecarIndex(dir, nil)
	_, _, ok := idx.Lookup(groupFor(base, base.Add(10*time.Second)))
	assert.False(t, ok)
}

func TestSidecarIndex_NonOverlappingSidecarIgnored(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	writeSidecar(t, dir, "a.json", sidecarFixture{
		SchemaVersion: 1,
		PeerAncestry:  []string{"xcodebuild"},
		AcceptedAt:    base,
		ClosedAt:      base.Add(5 * time.Second),
		Stats:         fixtureSts{Hits: 7},
	})

	idx := enrichment.NewSidecarIndex(dir, nil)
	_, _, ok := idx.Lookup(groupFor(base.Add(10*time.Second), base.Add(20*time.Second)))
	assert.False(t, ok)
}

func TestSidecarIndex_SingleMatchPopulatesStatsAndPath(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	path := writeSidecar(t, dir, "a.json", sidecarFixture{
		SchemaVersion: 1,
		PeerAncestry:  []string{"xcodebuild"},
		AcceptedAt:    base,
		ClosedAt:      base.Add(10 * time.Second),
		Stats: fixtureSts{
			Hits: 3, Misses: 2, KVHits: 5, KVMisses: 1,
			Uploads: 4, UploadBytes: 1024, DownloadBytes: 2048, KVUploadBytes: 256,
		},
	})

	idx := enrichment.NewSidecarIndex(dir, nil)
	stats, paths, ok := idx.Lookup(groupFor(base.Add(time.Second), base.Add(9*time.Second)))
	require.True(t, ok)
	assert.Equal(t, []string{path}, paths)
	assert.Equal(t, int64(3), stats.Hits)
	assert.Equal(t, int64(2), stats.Misses)
	assert.Equal(t, int64(5), stats.KVHits)
	assert.Equal(t, int64(1), stats.KVMisses)
	assert.Equal(t, int64(4), stats.Uploads)
	assert.Equal(t, int64(1024), stats.UploadBytes)
	assert.Equal(t, int64(2048), stats.DownloadBytes)
	assert.Equal(t, int64(256), stats.KVUploadBytes)
}

func TestSidecarIndex_TwoMatchesMergeAdditively(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	writeSidecar(t, dir, "a.json", sidecarFixture{
		SchemaVersion: 1,
		PeerAncestry:  []string{"xcodebuild"},
		AcceptedAt:    base,
		ClosedAt:      base.Add(5 * time.Second),
		Stats:         fixtureSts{Hits: 3, Misses: 1, UploadBytes: 10, DownloadBytes: 20},
	})
	writeSidecar(t, dir, "b.json", sidecarFixture{
		SchemaVersion: 1,
		PeerAncestry:  []string{"xcodebuild"},
		AcceptedAt:    base.Add(6 * time.Second),
		ClosedAt:      base.Add(10 * time.Second),
		Stats:         fixtureSts{Hits: 4, Misses: 2, UploadBytes: 7, DownloadBytes: 11},
	})

	idx := enrichment.NewSidecarIndex(dir, nil)
	stats, paths, ok := idx.Lookup(groupFor(base, base.Add(10*time.Second)))
	require.True(t, ok)
	assert.Len(t, paths, 2)
	assert.Equal(t, int64(7), stats.Hits)
	assert.Equal(t, int64(3), stats.Misses)
	assert.Equal(t, int64(17), stats.UploadBytes)
	assert.Equal(t, int64(31), stats.DownloadBytes)
}

func TestSidecarIndex_SchemaV2SkippedAndLeftOnDisk(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	futurePath := writeSidecar(t, dir, "future.json", sidecarFixture{
		SchemaVersion: 2,
		PeerAncestry:  []string{"xcodebuild"},
		AcceptedAt:    base,
		ClosedAt:      base.Add(5 * time.Second),
		Stats:         fixtureSts{Hits: 99},
	})

	idx := enrichment.NewSidecarIndex(dir, nil)
	_, _, ok := idx.Lookup(groupFor(base, base.Add(5*time.Second)))
	assert.False(t, ok)

	_, err := os.Stat(futurePath)
	assert.NoError(t, err, "schema v2 sidecar must remain on disk")
}

func TestSidecarIndex_CorruptJSONTolerated(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	corrupt := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(corrupt, []byte("{not-json"), 0o600))
	path := writeSidecar(t, dir, "good.json", sidecarFixture{
		SchemaVersion: 1,
		PeerAncestry:  []string{"xcodebuild"},
		AcceptedAt:    base,
		ClosedAt:      base.Add(5 * time.Second),
		Stats:         fixtureSts{Hits: 1},
	})

	idx := enrichment.NewSidecarIndex(dir, nil)
	stats, paths, ok := idx.Lookup(groupFor(base, base.Add(5*time.Second)))
	require.True(t, ok)
	assert.Equal(t, []string{path}, paths)
	assert.Equal(t, int64(1), stats.Hits)

	_, err := os.Stat(corrupt)
	assert.NoError(t, err, "corrupt sidecar must stay on disk for later inspection")
}
