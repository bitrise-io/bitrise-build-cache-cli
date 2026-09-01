//go:build unit

package common

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

func TestReadProjectMarker_HappyPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".bitrise-build-cache.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
  "workspace": "acme-corp",
  "push": true,
  "tools": {
    "gradle": {"enabled": true},
    "ccache": {"enabled": false}
  }
}`), 0o644))

	marker, err := ReadProjectMarker(path, utils.DefaultOsProxy{})

	require.NoError(t, err)
	require.NotNil(t, marker)
	assert.Equal(t, "acme-corp", marker.Workspace)
	require.NotNil(t, marker.Push)
	assert.True(t, *marker.Push)
	require.Contains(t, marker.Tools, "gradle")
	require.NotNil(t, marker.Tools["gradle"].Enabled)
	assert.True(t, *marker.Tools["gradle"].Enabled)
	require.NotNil(t, marker.Tools["ccache"].Enabled)
	assert.False(t, *marker.Tools["ccache"].Enabled)
}

func TestReadProjectMarker_MinimalValid(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".bitrise-build-cache.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"workspace":"acme-corp"}`), 0o644))

	marker, err := ReadProjectMarker(path, utils.DefaultOsProxy{})

	require.NoError(t, err)
	require.NotNil(t, marker)
	assert.Equal(t, "acme-corp", marker.Workspace)
	assert.Nil(t, marker.Push)
	assert.Empty(t, marker.Tools)
}

func TestReadProjectMarker_MissingFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".bitrise-build-cache.json")

	marker, err := ReadProjectMarker(path, utils.DefaultOsProxy{})

	require.NoError(t, err)
	assert.Nil(t, marker)
}

func TestReadProjectMarker_MalformedJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".bitrise-build-cache.json")
	require.NoError(t, os.WriteFile(path, []byte(`{not valid json`), 0o644))

	marker, err := ReadProjectMarker(path, utils.DefaultOsProxy{})

	require.Error(t, err)
	assert.Nil(t, marker)
}

func TestReadProjectMarker_MissingWorkspace(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".bitrise-build-cache.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"push":true}`), 0o644))

	marker, err := ReadProjectMarker(path, utils.DefaultOsProxy{})

	require.Error(t, err)
	assert.Nil(t, marker)
	assert.True(t, errors.Is(err, ErrProjectMarkerMissingWorkspace))
}

func TestReadProjectMarker_EmptyWorkspace(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".bitrise-build-cache.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"workspace":""}`), 0o644))

	marker, err := ReadProjectMarker(path, utils.DefaultOsProxy{})

	require.Error(t, err)
	assert.Nil(t, marker)
	assert.True(t, errors.Is(err, ErrProjectMarkerMissingWorkspace))
}

func TestReadProjectMarker_ReadError(t *testing.T) {
	t.Parallel()

	proxy := &mocks.OsProxyMock{
		ReadFileIfExistsFunc: func(string) (string, bool, error) {
			return "", true, errors.New("boom")
		},
	}

	marker, err := ReadProjectMarker("/some/path", proxy)

	require.Error(t, err)
	assert.Nil(t, marker)
}

func TestWalkUpFindMarker_FoundAtStartDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".bitrise-build-cache.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"workspace":"acme"}`), 0o644))

	foundPath, marker, err := WalkUpFindMarker(dir, utils.DefaultOsProxy{})

	require.NoError(t, err)
	require.NotNil(t, marker)
	assert.Equal(t, "acme", marker.Workspace)
	assert.Equal(t, path, foundPath)
}

func TestWalkUpFindMarker_FoundAtParent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	nested := filepath.Join(root, "sub", "sub2", "sub3")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	markerPath := filepath.Join(root, ".bitrise-build-cache.json")
	require.NoError(t, os.WriteFile(markerPath, []byte(`{"workspace":"acme"}`), 0o644))

	foundPath, marker, err := WalkUpFindMarker(nested, utils.DefaultOsProxy{})

	require.NoError(t, err)
	require.NotNil(t, marker)
	assert.Equal(t, "acme", marker.Workspace)
	assert.Equal(t, markerPath, foundPath)
}

func TestWalkUpFindMarker_StopsAtNearestMarker(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mid := filepath.Join(root, "mid")
	leaf := filepath.Join(mid, "leaf")
	require.NoError(t, os.MkdirAll(leaf, 0o755))

	rootMarker := filepath.Join(root, ".bitrise-build-cache.json")
	midMarker := filepath.Join(mid, ".bitrise-build-cache.json")
	require.NoError(t, os.WriteFile(rootMarker, []byte(`{"workspace":"outer"}`), 0o644))
	require.NoError(t, os.WriteFile(midMarker, []byte(`{"workspace":"inner"}`), 0o644))

	foundPath, marker, err := WalkUpFindMarker(leaf, utils.DefaultOsProxy{})

	require.NoError(t, err)
	require.NotNil(t, marker)
	assert.Equal(t, "inner", marker.Workspace)
	assert.Equal(t, midMarker, foundPath)
}

func TestWalkUpFindMarker_NoneFound(t *testing.T) {
	t.Parallel()

	proxy := &mocks.OsProxyMock{
		ReadFileIfExistsFunc: func(string) (string, bool, error) {
			return "", false, nil
		},
	}

	foundPath, marker, err := WalkUpFindMarker("/does/not/matter/a/b/c", proxy)

	require.NoError(t, err)
	assert.Nil(t, marker)
	assert.Empty(t, foundPath)
}

func TestWalkUpFindMarker_MalformedMarkerStops(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, ".bitrise-build-cache.json"), []byte(`bad`), 0o644))

	foundPath, marker, err := WalkUpFindMarker(sub, utils.DefaultOsProxy{})

	require.Error(t, err)
	assert.Nil(t, marker)
	assert.Empty(t, foundPath)
}
