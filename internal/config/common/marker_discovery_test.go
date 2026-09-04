//go:build unit

package common_test

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func writeMarker(t *testing.T, dir, workspace string) {
	t.Helper()
	require.NoError(t, utils.DefaultOsProxy{}.WriteFile(
		filepath.Join(dir, ".bitrise-build-cache.json"),
		[]byte(`{"workspace":"`+workspace+`"}`),
		0o600,
	))
}

func TestDiscoverWorkspaceSlug_MarkerAtStartDir(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "acme")

	assert.Equal(t, "acme", configcommon.DiscoverWorkspaceSlug(dir, utils.DefaultOsProxy{}, io.Discard))
}

// A mis-set CWD (e.g. a subdir) must still find a marker sitting at the
// workspace root.
func TestDiscoverWorkspaceSlug_MarkerInParent(t *testing.T) {
	root := t.TempDir()
	writeMarker(t, root, "parent-ws")
	sub := filepath.Join(root, "packages", "app")
	require.NoError(t, utils.DefaultOsProxy{}.MkdirAll(sub, 0o700))

	assert.Equal(t, "parent-ws", configcommon.DiscoverWorkspaceSlug(sub, utils.DefaultOsProxy{}, io.Discard))
}

func TestDiscoverWorkspaceSlug_NoMarker_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()

	warn := &bytes.Buffer{}
	assert.Empty(t, configcommon.DiscoverWorkspaceSlug(dir, utils.DefaultOsProxy{}, warn))
	assert.Empty(t, warn.String(), "an absent marker is the normal path, not a warning")
}

// A broken marker must NOT block auth; it warns and falls through to the
// machine-wide credential.
func TestDiscoverWorkspaceSlug_MalformedMarker_WarnsAndReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, utils.DefaultOsProxy{}.WriteFile(
		filepath.Join(dir, ".bitrise-build-cache.json"),
		[]byte(`{not json`),
		0o600,
	))

	warn := &bytes.Buffer{}
	assert.Empty(t, configcommon.DiscoverWorkspaceSlug(dir, utils.DefaultOsProxy{}, warn))
	assert.Contains(t, warn.String(), "project marker")
}

// A marker missing the required workspace field surfaces as a read error from
// WalkUpFindMarker; the caller must still fall back to machine-wide auth.
func TestDiscoverWorkspaceSlug_MarkerMissingWorkspace_WarnsAndReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, utils.DefaultOsProxy{}.WriteFile(
		filepath.Join(dir, ".bitrise-build-cache.json"),
		[]byte(`{}`),
		0o600,
	))

	warn := &bytes.Buffer{}
	assert.Empty(t, configcommon.DiscoverWorkspaceSlug(dir, utils.DefaultOsProxy{}, warn))
	assert.Contains(t, warn.String(), "project marker")
}
