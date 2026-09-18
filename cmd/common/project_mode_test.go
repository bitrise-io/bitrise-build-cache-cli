//go:build unit

package common

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func TestResolveAndPersistProjectMode_OptInWritesMarkerAtCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	build := filepath.Join(home, "some-project")
	require.NoError(t, os.MkdirAll(build, 0o755))
	t.Chdir(build)

	mode, err := ResolveAndPersistProjectMode(string(machineconfig.ModeOptIn), nil)
	require.NoError(t, err)
	assert.Equal(t, machineconfig.ModeOptIn, mode)

	_, err = os.Stat(filepath.Join(build, paths.ProjectMarkerFilename))
	require.NoError(t, err, "activate must drop the marker at cwd on opt-in")
}

func TestResolveAndPersistProjectMode_AlwaysDoesNotWriteMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	build := filepath.Join(home, "any-project")
	require.NoError(t, os.MkdirAll(build, 0o755))
	t.Chdir(build)

	_, err := ResolveAndPersistProjectMode(string(machineconfig.ModeAlways), nil)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(build, paths.ProjectMarkerFilename))
	assert.True(t, os.IsNotExist(err), "always mode must not drop a marker")
}

func TestResolveAndPersistProjectMode_OptInAncestorMarkerSkipsWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	parent := filepath.Join(home, "parent")
	child := filepath.Join(parent, "child")
	require.NoError(t, os.MkdirAll(child, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(parent, paths.ProjectMarkerFilename), []byte(`{}`), 0o644))
	t.Chdir(child)

	_, err := ResolveAndPersistProjectMode(string(machineconfig.ModeOptIn), nil)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(child, paths.ProjectMarkerFilename))
	assert.True(t, os.IsNotExist(err), "child must not get a marker when ancestor already covers it")
}
