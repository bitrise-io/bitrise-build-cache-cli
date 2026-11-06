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
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/sessions"
)

func writeSidecarForProxy(t *testing.T, dir, name string, s sessions.Sidecar) string {
	t.Helper()
	body, err := json.Marshal(s)
	require.NoError(t, err)
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, body, 0o600))

	return path
}

func blobOne() *blobstats.Snapshot {
	return &blobstats.Snapshot{
		Download: blobstats.DirectionSnapshot{OpCount: 1},
	}
}

func TestGroupSidecarsByProxyID_DarkFoldsIntoMain(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	dd := "/x/DD"

	// Main — lands first.
	mainPath := writeSidecarForProxy(t, dir, "main.json", sessions.Sidecar{
		SchemaVersion:      3,
		SidecarUUID:        "sc-main",
		PeerPID:            10,
		PeerAncestry:       []string{"xcodebuild"},
		AcceptedAt:         base,
		ClosedAt:           base.Add(5 * time.Second),
		BlobStats:          blobOne(),
		WrapperSession:     &sessions.WrapperLink{InvocationID: "inv-main"},
		DerivedDataPath:    dd,
		BuildRequestAction: "build",
		Terminal:           true,
	})
	// Dark — same DD, different proxy ID, action=build qos=default.
	darkPath := writeSidecarForProxy(t, dir, "dark.json", sessions.Sidecar{
		SchemaVersion:      3,
		SidecarUUID:        "sc-dark",
		PeerPID:            11,
		PeerAncestry:       []string{"xcodebuild"},
		AcceptedAt:         base.Add(time.Second),
		ClosedAt:           base.Add(4 * time.Second),
		BlobStats:          blobOne(),
		WrapperSession:     &sessions.WrapperLink{InvocationID: "inv-dark"},
		DerivedDataPath:    dd,
		BuildRequestAction: "build",
		BuildRequestQoS:    "default",
	})

	groups := enrichment.GroupSidecarsByProxyID(dir, nil)
	require.Len(t, groups, 1, "dark must fold into main")
	g := groups["inv-main"]
	require.NotNil(t, g)
	assert.Equal(t, enrichment.RowMainBuild, g.Primary)
	assert.ElementsMatch(t, []string{mainPath, darkPath}, g.SidecarPaths)
	assert.Equal(t, int64(2), g.Blob.Download.OpCount, "blobs sum across folded dark")
	assert.True(t, g.Terminal)
}

func TestGroupSidecarsByProxyID_DarkWithoutMainKeepsOwnGroup(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

	writeSidecarForProxy(t, dir, "dark.json", sessions.Sidecar{
		SchemaVersion:      3,
		SidecarUUID:        "sc-dark",
		PeerPID:            11,
		PeerAncestry:       []string{"xcodebuild"},
		AcceptedAt:         base,
		ClosedAt:           base.Add(4 * time.Second),
		BlobStats:          blobOne(),
		WrapperSession:     &sessions.WrapperLink{InvocationID: "inv-dark"},
		DerivedDataPath:    "/x/DD",
		BuildRequestAction: "build",
		BuildRequestQoS:    "default",
	})

	groups := enrichment.GroupSidecarsByProxyID(dir, nil)
	require.Len(t, groups, 1)
	g := groups["inv-dark"]
	require.NotNil(t, g)
	assert.Equal(t, enrichment.RowDarkBuild, g.Primary)
}

func TestGroupSidecarsByProxyID_IndexerIsOwnGroup(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

	writeSidecarForProxy(t, dir, "idx.json", sessions.Sidecar{
		SchemaVersion:      3,
		SidecarUUID:        "sc-idx",
		PeerPID:            12,
		PeerAncestry:       []string{"xcodebuild"},
		AcceptedAt:         base,
		ClosedAt:           base.Add(4 * time.Second),
		BlobStats:          blobOne(),
		WrapperSession:     &sessions.WrapperLink{InvocationID: "inv-idx"},
		DerivedDataPath:    "/x/DD",
		BuildRequestAction: "indexbuild",
		BuildRequestQoS:    "prepareForIndexing",
	})

	groups := enrichment.GroupSidecarsByProxyID(dir, nil)
	require.Len(t, groups, 1)
	assert.Equal(t, enrichment.RowIndexer, groups["inv-idx"].Primary)
}

func TestGroupSidecarsByProxyID_SkipsAnchorlessAndWrongSchema(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

	// anchorless
	writeSidecarForProxy(t, dir, "anchorless.json", sessions.Sidecar{
		SchemaVersion: 3,
		SidecarUUID:   "a",
		PeerAncestry:  []string{"xcodebuild"},
		AcceptedAt:    base,
		ClosedAt:      base.Add(time.Second),
	})
	// old schema
	writeSidecarForProxy(t, dir, "oldschema.json", sessions.Sidecar{
		SchemaVersion:  2,
		SidecarUUID:    "b",
		PeerAncestry:   []string{"xcodebuild"},
		AcceptedAt:     base,
		ClosedAt:       base.Add(time.Second),
		WrapperSession: &sessions.WrapperLink{InvocationID: "wont-see"},
	})

	groups := enrichment.GroupSidecarsByProxyID(dir, nil)
	assert.Empty(t, groups)
}
