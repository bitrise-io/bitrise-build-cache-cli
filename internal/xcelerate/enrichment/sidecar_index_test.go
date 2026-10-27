//go:build unit

package enrichment_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

type sidecarFixture struct {
	SchemaVersion  int                    `json:"schema_version"`
	SidecarUUID    string                 `json:"sidecar_uuid,omitempty"`
	PeerAncestry   []string               `json:"peer_ancestry"`
	AcceptedAt     time.Time              `json:"accepted_at"`
	ClosedAt       time.Time              `json:"closed_at"`
	BlobStats      *blobstats.Snapshot    `json:"blobStats,omitempty"`
	WrapperSession *sidecarFixtureWrapper `json:"wrapper_session,omitempty"`
}

type sidecarFixtureWrapper struct {
	InvocationID string `json:"invocation_id"`
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

	snap, paths, _, ok := idx.Lookup(groupFor(time.Now(), time.Now().Add(time.Second)))
	assert.False(t, ok)
	assert.Empty(t, paths)
	assert.Nil(t, snap)
}

func TestSidecarIndex_MissingDirReturnsNotFound(t *testing.T) {
	idx := enrichment.NewSidecarIndex(filepath.Join(t.TempDir(), "nope"), nil)

	_, _, _, ok := idx.Lookup(groupFor(time.Now(), time.Now().Add(time.Second)))
	assert.False(t, ok)
}

func TestSidecarIndex_EmptyDirStringReturnsNotFound(t *testing.T) {
	idx := enrichment.NewSidecarIndex("", nil)

	_, _, _, ok := idx.Lookup(groupFor(time.Now(), time.Now().Add(time.Second)))
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
	_, _, _, ok := idx.Lookup(groupFor(base, base.Add(10*time.Second)))
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
	_, _, _, ok := idx.Lookup(groupFor(base.Add(10*time.Second), base.Add(20*time.Second)))
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
	snap, paths, _, ok := idx.Lookup(groupFor(base.Add(time.Second), base.Add(9*time.Second)))
	require.True(t, ok)
	assert.Equal(t, []string{path}, paths)
	require.NotNil(t, snap)
	assert.Equal(t, int64(3), snap.Download.OpCount)
	assert.Equal(t, int64(2), snap.Download.MissCount)
	assert.Equal(t, int64(4), snap.Upload.OpCount)
}

func TestSidecarIndex_WrapperLinkInvocationIDSurfaced(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	writeSidecar(t, dir, "a.json", sidecarFixture{
		SchemaVersion:  2,
		PeerAncestry:   []string{"xcodebuild"},
		AcceptedAt:     base,
		ClosedAt:       base.Add(5 * time.Second),
		BlobStats:      blob(1, 0, 0),
		WrapperSession: &sidecarFixtureWrapper{InvocationID: "deadbeef-1111-4222-8333-444455556666"},
	})

	idx := enrichment.NewSidecarIndex(dir, nil)
	_, _, invocationID, ok := idx.Lookup(groupFor(base, base.Add(5*time.Second)))
	require.True(t, ok)
	assert.Equal(t, "deadbeef-1111-4222-8333-444455556666", invocationID, "WrapperLink.InvocationID must be surfaced to callers")
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
	snap, paths, _, ok := idx.Lookup(groupFor(base, base.Add(10*time.Second)))
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
	_, _, _, ok := idx.Lookup(groupFor(base, base.Add(5*time.Second)))
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
	snap, paths, _, ok := idx.Lookup(groupFor(base, base.Add(5*time.Second)))
	require.True(t, ok)
	assert.Equal(t, []string{path}, paths)
	require.NotNil(t, snap)
	assert.Equal(t, int64(1), snap.Download.OpCount)

	_, err := os.Stat(corrupt)
	assert.NoError(t, err, "corrupt sidecar must stay on disk for later inspection")
}

func TestSidecarIndex_OrphanSidecars_GroupsByInvocationID(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)

	writeSidecar(t, dir, "a.json", sidecarFixture{
		SchemaVersion:  2,
		SidecarUUID:    "sidecar-a",
		PeerAncestry:   []string{"xcodebuild"},
		AcceptedAt:     base,
		ClosedAt:       base.Add(5 * time.Second),
		BlobStats:      blob(3, 1, 2),
		WrapperSession: &sidecarFixtureWrapper{InvocationID: "invocation-X"},
	})
	writeSidecar(t, dir, "b.json", sidecarFixture{
		SchemaVersion:  2,
		SidecarUUID:    "sidecar-b",
		PeerAncestry:   []string{"xcodebuild"},
		AcceptedAt:     base.Add(2 * time.Second),
		ClosedAt:       base.Add(7 * time.Second),
		BlobStats:      blob(4, 2, 1),
		WrapperSession: &sidecarFixtureWrapper{InvocationID: "invocation-X"},
	})
	writeSidecar(t, dir, "c.json", sidecarFixture{
		SchemaVersion:  2,
		SidecarUUID:    "sidecar-c",
		PeerAncestry:   []string{"xcodebuild"},
		AcceptedAt:     base,
		ClosedAt:       base.Add(5 * time.Second),
		BlobStats:      blob(1, 0, 0),
		WrapperSession: &sidecarFixtureWrapper{InvocationID: "invocation-Y"},
	})

	idx := enrichment.NewSidecarIndex(dir, nil)
	// Fire well after ClosedAt so minAge gate is satisfied.
	now := base.Add(30 * time.Second)
	groups := idx.OrphanSidecarsGroupedByInvocation(now, time.Second)

	require.Len(t, groups, 2, "two distinct InvocationIDs must land in two buckets")
	gX := groups["invocation-X"]
	require.NotNil(t, gX.Blob)
	assert.Equal(t, int64(7), gX.Blob.Download.OpCount, "download OpCounts sum across sidecars sharing an ID")
	assert.Equal(t, int64(3), gX.Blob.Download.MissCount)
	assert.Equal(t, int64(3), gX.Blob.Upload.OpCount)
	assert.ElementsMatch(t, []string{"sidecar-a", "sidecar-b"}, gX.SidecarUUIDs)
	assert.Len(t, gX.Paths, 2)
	assert.Equal(t, base, gX.Start, "Start is earliest AcceptedAt in the group")
	assert.Equal(t, base.Add(7*time.Second), gX.Stop, "Stop is latest ClosedAt in the group")

	gY := groups["invocation-Y"]
	require.NotNil(t, gY.Blob)
	assert.Equal(t, int64(1), gY.Blob.Download.OpCount)
	assert.Equal(t, []string{"sidecar-c"}, gY.SidecarUUIDs)
}

func TestSidecarIndex_OrphanSidecars_AgeGateFiltersFreshSidecars(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)

	writeSidecar(t, dir, "fresh.json", sidecarFixture{
		SchemaVersion:  2,
		SidecarUUID:    "sidecar-fresh",
		PeerAncestry:   []string{"xcodebuild"},
		AcceptedAt:     base,
		ClosedAt:       base.Add(5 * time.Second),
		BlobStats:      blob(1, 0, 0),
		WrapperSession: &sidecarFixtureWrapper{InvocationID: "invocation-F"},
	})

	idx := enrichment.NewSidecarIndex(dir, nil)
	// Only 1s past ClosedAt — minAge gate must drop it.
	now := base.Add(6 * time.Second)
	groups := idx.OrphanSidecarsGroupedByInvocation(now, 10*time.Second)
	assert.Empty(t, groups, "sidecar closed within minAge window must not surface")
}

func TestSidecarIndex_OrphanSidecars_IgnoresSidecarsWithoutInvocationID(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)

	writeSidecar(t, dir, "unanchored.json", sidecarFixture{
		SchemaVersion: 2,
		SidecarUUID:   "sidecar-no-id",
		PeerAncestry:  []string{"xcodebuild"},
		AcceptedAt:    base,
		ClosedAt:      base.Add(5 * time.Second),
		BlobStats:     blob(5, 0, 0),
		// WrapperSession nil — peer-ancestry anchoring failed on the proxy side.
	})
	writeSidecar(t, dir, "blank-id.json", sidecarFixture{
		SchemaVersion:  2,
		SidecarUUID:    "sidecar-blank",
		PeerAncestry:   []string{"xcodebuild"},
		AcceptedAt:     base,
		ClosedAt:       base.Add(5 * time.Second),
		BlobStats:      blob(5, 0, 0),
		WrapperSession: &sidecarFixtureWrapper{InvocationID: ""},
	})

	idx := enrichment.NewSidecarIndex(dir, nil)
	now := base.Add(60 * time.Second)
	groups := idx.OrphanSidecarsGroupedByInvocation(now, time.Second)
	assert.Empty(t, groups, "sidecars without a populated InvocationID must not surface as orphans")
}

func TestSidecarIndex_OrphanSidecars_WarnsOnAgedSidecarsWithoutInvocationID(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)

	writeSidecar(t, dir, "aged-anchored.json", sidecarFixture{
		SchemaVersion:  2,
		SidecarUUID:    "sidecar-anchored",
		PeerAncestry:   []string{"xcodebuild"},
		AcceptedAt:     base,
		ClosedAt:       base.Add(5 * time.Second),
		BlobStats:      blob(1, 0, 0),
		WrapperSession: &sidecarFixtureWrapper{InvocationID: "invocation-X"},
	})
	writeSidecar(t, dir, "aged-unanchored.json", sidecarFixture{
		SchemaVersion: 2,
		SidecarUUID:   "sidecar-unanchored",
		PeerAncestry:  []string{"xcodebuild"},
		AcceptedAt:    base,
		ClosedAt:      base.Add(5 * time.Second),
		BlobStats:     blob(5, 0, 0),
		// WrapperSession nil — peer-ancestry anchoring failed on the proxy side.
	})
	writeSidecar(t, dir, "fresh-unanchored.json", sidecarFixture{
		SchemaVersion: 2,
		SidecarUUID:   "sidecar-fresh",
		PeerAncestry:  []string{"xcodebuild"},
		AcceptedAt:    base.Add(58 * time.Second),
		ClosedAt:      base.Add(59 * time.Second),
		BlobStats:     blob(1, 0, 0),
		// Also unanchored but still inside the age window — must NOT contribute to the warn.
	})

	buf := &bytes.Buffer{}
	idx := enrichment.NewSidecarIndex(dir, log.NewLogger(log.WithOutput(buf)))
	now := base.Add(60 * time.Second)
	groups := idx.OrphanSidecarsGroupedByInvocation(now, 10*time.Second)

	require.Len(t, groups, 1, "only the anchored aged sidecar must surface")
	assert.Contains(t, buf.String(), "1 aged sidecar(s) have no WrapperSession.InvocationID",
		"warn must fire for aged orphan sidecars that will never land in BE; fresh ones are excluded")
}

func TestSidecarIndex_OrphanSidecars_SchemaFilterRespected(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)

	writeSidecar(t, dir, "future.json", sidecarFixture{
		SchemaVersion:  3,
		SidecarUUID:    "sidecar-future",
		PeerAncestry:   []string{"xcodebuild"},
		AcceptedAt:     base,
		ClosedAt:       base.Add(5 * time.Second),
		BlobStats:      blob(1, 0, 0),
		WrapperSession: &sidecarFixtureWrapper{InvocationID: "invocation-X"},
	})
	writeSidecar(t, dir, "wrong-ancestry.json", sidecarFixture{
		SchemaVersion:  2,
		SidecarUUID:    "sidecar-wrong",
		PeerAncestry:   []string{"ssh"},
		AcceptedAt:     base,
		ClosedAt:       base.Add(5 * time.Second),
		BlobStats:      blob(1, 0, 0),
		WrapperSession: &sidecarFixtureWrapper{InvocationID: "invocation-Y"},
	})

	idx := enrichment.NewSidecarIndex(dir, nil)
	now := base.Add(60 * time.Second)
	groups := idx.OrphanSidecarsGroupedByInvocation(now, time.Second)
	assert.Empty(t, groups, "schema-too-new and ancestry-rejected sidecars must not surface")
}
