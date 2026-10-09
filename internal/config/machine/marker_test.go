//go:build unit

package machine

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

func TestFindMarker_FoundAtStartDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeMarker(t, dir)

	found, path, err := FindMarker(dir, utils.DefaultOsProxy{})

	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, filepath.Join(dir, paths.ProjectMarkerFilename), path)
}

func TestFindMarker_FoundAtParent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeMarker(t, root)
	sub := filepath.Join(root, "nested", "leaf")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	found, path, err := FindMarker(sub, utils.DefaultOsProxy{})

	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, filepath.Join(root, paths.ProjectMarkerFilename), path)
}

func TestFindMarker_NoneFound(t *testing.T) {
	t.Parallel()

	proxy := &mocks.OsProxyMock{
		ReadFileIfExistsFunc: func(string) (string, bool, error) {
			return "", false, nil
		},
	}

	found, path, err := FindMarker("/does/not/matter", proxy)

	require.NoError(t, err)
	assert.False(t, found)
	assert.Empty(t, path)
}

func TestFindMarker_MalformedMarkerErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, paths.ProjectMarkerFilename), []byte(`{not json`), 0o644))

	found, path, err := FindMarker(dir, utils.DefaultOsProxy{})

	require.Error(t, err)
	assert.False(t, found)
	assert.Empty(t, path)
}

func TestReadMarker_ReadErrorSurfaces(t *testing.T) {
	t.Parallel()

	proxy := &mocks.OsProxyMock{
		ReadFileIfExistsFunc: func(string) (string, bool, error) {
			return "", true, errors.New("boom")
		},
	}

	marker, err := ReadMarker("/some/path", proxy)

	require.Error(t, err)
	assert.Nil(t, marker)
}

func TestRead_MissingFileResolvesToAlways(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	cfg, err := Read(utils.DefaultOsProxy{}, paths.FromHome(home), nil)
	require.NoError(t, err)
	assert.Equal(t, ModeAlways, ResolvedProjectMode(cfg))
}

func TestRead_ReturnsStoredValue(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	p := paths.FromHome(home)
	require.NoError(t, Write(Config{ProjectMode: ModeOptIn}, utils.DefaultOsProxy{}, p))

	cfg, err := Read(utils.DefaultOsProxy{}, p, nil)
	require.NoError(t, err)
	assert.Equal(t, ModeOptIn, ResolvedProjectMode(cfg))
}

func TestWriteMarkerIfMissing_FreshWriteDropsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	wrote, ancestor, err := WriteMarkerIfMissing(dir, utils.DefaultOsProxy{})

	require.NoError(t, err)
	assert.Empty(t, ancestor)
	assert.Equal(t, filepath.Join(dir, paths.ProjectMarkerFilename), wrote)

	body, err := os.ReadFile(wrote) //nolint:gosec // test tempdir
	require.NoError(t, err)
	assert.Equal(t, "{}\n", string(body))
}

func TestWriteMarkerIfMissing_ExistingMarkerAtDirIsNoop(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, paths.ProjectMarkerFilename)
	original := []byte(`{"workspace":"keep"}`)
	require.NoError(t, os.WriteFile(target, original, 0o644))

	wrote, ancestor, err := WriteMarkerIfMissing(dir, utils.DefaultOsProxy{})

	require.NoError(t, err)
	assert.Empty(t, ancestor)
	assert.Equal(t, target, wrote, "cwd-level marker is treated as a fresh-write result")

	body, err := os.ReadFile(target) //nolint:gosec // test tempdir
	require.NoError(t, err)
	assert.Equal(t, string(original), string(body), "existing user-authored body must be preserved")
}

func TestWriteMarkerIfMissing_AncestorMarkerReportsCoverage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeMarker(t, root)
	sub := filepath.Join(root, "child")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	wrote, ancestor, err := WriteMarkerIfMissing(sub, utils.DefaultOsProxy{})

	require.NoError(t, err)
	assert.Empty(t, wrote)
	assert.Equal(t, filepath.Join(root, paths.ProjectMarkerFilename), ancestor)

	_, statErr := os.Stat(filepath.Join(sub, paths.ProjectMarkerFilename))
	assert.True(t, os.IsNotExist(statErr), "no new file should land when an ancestor already covers")
}

func TestWriteMarkerIfMissing_UnwritableDirErrors(t *testing.T) {
	t.Parallel()

	proxy := &mocks.OsProxyMock{
		ReadFileIfExistsFunc: func(string) (string, bool, error) { return "", false, nil },
		WriteFileFunc:        func(string, []byte, os.FileMode) error { return errors.New("permission denied") },
	}

	wrote, ancestor, err := WriteMarkerIfMissing("/doesnotmatter", proxy)

	require.Error(t, err)
	assert.Empty(t, wrote)
	assert.Empty(t, ancestor)
}

func writeMarker(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, paths.ProjectMarkerFilename), []byte(`{}`), 0o644))
}
