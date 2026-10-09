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

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

type sidecarFixture struct {
	SchemaVersion int                 `json:"schema_version"`
	PeerAncestry  []string            `json:"peer_ancestry"`
	AcceptedAt    time.Time           `json:"accepted_at"`
	ClosedAt      time.Time           `json:"closed_at"`
	BlobStats     *blobstats.Snapshot `json:"blobStats,omitempty"`
}

func blob(downloadOps, downloadMiss, uploadOps int64) *blobstats.Snapshot {
	return &blobstats.Snapshot{
		Download: blobstats.DirectionSnapshot{OpCount: downloadOps, MissCount: downloadMiss},
		Upload:   blobstats.DirectionSnapshot{OpCount: uploadOps},
	}
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

	snap, paths, ok := idx.Lookup(groupFor(time.Now(), time.Now().Add(time.Second)))
	assert.False(t, ok)
	assert.Empty(t, paths)
	assert.Nil(t, snap)
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
		SchemaVersion: 2,
		PeerAncestry:  []string{"swift-driver", "clang"},
		AcceptedAt:    base,
		ClosedAt:      base.Add(10 * time.Second),
		BlobStats:     blob(7, 0, 0),
	})

	idx := enrichment.NewSidecarIndex(dir, nil)
	_, _, ok := idx.Lookup(groupFor(base, base.Add(10*time.Second)))
	assert.False(t, ok)
}

func TestSidecarIndex_NonOverlappingSidecarIgnored(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	writeSidecar(t, dir, "a.json", sidecarFixture{
		SchemaVersion: 2,
		PeerAncestry:  []string{"xcodebuild"},
		AcceptedAt:    base,
		ClosedAt:      base.Add(5 * time.Second),
		BlobStats:     blob(7, 0, 0),
	})

	idx := enrichment.NewSidecarIndex(dir, nil)
	_, _, ok := idx.Lookup(groupFor(base.Add(10*time.Second), base.Add(20*time.Second)))
	assert.False(t, ok)
}

func TestSidecarIndex_SingleMatchPopulatesBlobAndPath(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	path := writeSidecar(t, dir, "a.json", sidecarFixture{
		SchemaVersion: 2,
		PeerAncestry:  []string{"xcodebuild"},
		AcceptedAt:    base,
		ClosedAt:      base.Add(10 * time.Second),
		BlobStats:     blob(3, 2, 4),
	})

	idx := enrichment.NewSidecarIndex(dir, nil)
	snap, paths, ok := idx.Lookup(groupFor(base.Add(time.Second), base.Add(9*time.Second)))
	require.True(t, ok)
	assert.Equal(t, []string{path}, paths)
	require.NotNil(t, snap)
	assert.Equal(t, int64(3), snap.Download.OpCount)
	assert.Equal(t, int64(2), snap.Download.MissCount)
	assert.Equal(t, int64(4), snap.Upload.OpCount)
}

func TestSidecarIndex_TwoMatchesPickLargestBlob(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	writeSidecar(t, dir, "small.json", sidecarFixture{
		SchemaVersion: 2,
		PeerAncestry:  []string{"xcodebuild"},
		AcceptedAt:    base,
		ClosedAt:      base.Add(5 * time.Second),
		BlobStats:     blob(2, 1, 1),
	})
	writeSidecar(t, dir, "large.json", sidecarFixture{
		SchemaVersion: 2,
		PeerAncestry:  []string{"xcodebuild"},
		AcceptedAt:    base.Add(6 * time.Second),
		ClosedAt:      base.Add(10 * time.Second),
		BlobStats:     blob(9, 2, 3),
	})

	idx := enrichment.NewSidecarIndex(dir, nil)
	snap, paths, ok := idx.Lookup(groupFor(base, base.Add(10*time.Second)))
	require.True(t, ok)
	assert.Len(t, paths, 2, "both overlapping sidecars must be unlinked")
	require.NotNil(t, snap)
	assert.Equal(t, int64(9), snap.Download.OpCount, "largest-op blob wins tie-break")
	assert.Equal(t, int64(2), snap.Download.MissCount)
	assert.Equal(t, int64(3), snap.Upload.OpCount)
}

func TestSidecarIndex_FutureSchemaSkippedAndLeftOnDisk(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	futurePath := writeSidecar(t, dir, "future.json", sidecarFixture{
		SchemaVersion: 3,
		PeerAncestry:  []string{"xcodebuild"},
		AcceptedAt:    base,
		ClosedAt:      base.Add(5 * time.Second),
		BlobStats:     blob(99, 0, 0),
	})

	idx := enrichment.NewSidecarIndex(dir, nil)
	_, _, ok := idx.Lookup(groupFor(base, base.Add(5*time.Second)))
	assert.False(t, ok)

	_, err := os.Stat(futurePath)
	assert.NoError(t, err, "future-schema sidecar must remain on disk")
}

func TestSidecarIndex_CorruptJSONTolerated(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	corrupt := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(corrupt, []byte("{not-json"), 0o600))
	path := writeSidecar(t, dir, "good.json", sidecarFixture{
		SchemaVersion: 2,
		PeerAncestry:  []string{"xcodebuild"},
		AcceptedAt:    base,
		ClosedAt:      base.Add(5 * time.Second),
		BlobStats:     blob(1, 0, 0),
	})

	idx := enrichment.NewSidecarIndex(dir, nil)
	snap, paths, ok := idx.Lookup(groupFor(base, base.Add(5*time.Second)))
	require.True(t, ok)
	assert.Equal(t, []string{path}, paths)
	require.NotNil(t, snap)
	assert.Equal(t, int64(1), snap.Download.OpCount)

	_, err := os.Stat(corrupt)
	assert.NoError(t, err, "corrupt sidecar must stay on disk for later inspection")
}
