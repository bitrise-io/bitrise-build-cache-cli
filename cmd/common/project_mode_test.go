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
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func TestPersistProjectMode_EmptyFlagIsNoop(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.NoError(t, PersistProjectMode("", nil))

	_, statErr := os.Stat(paths.FromHome(home).MachineConfigFile())
	assert.True(t, os.IsNotExist(statErr), "empty flag must not create machine config")
}

func TestPersistProjectMode_WritesFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.NoError(t, PersistProjectMode(string(machineconfig.ModeOptIn), nil))

	cfg, err := machineconfig.Read(utils.DefaultOsProxy{}, paths.FromHome(home), nil)
	require.NoError(t, err)
	assert.Equal(t, machineconfig.ModeOptIn, cfg.ProjectMode)
}

func TestPersistProjectMode_InvalidFlagReturnsError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	err := PersistProjectMode("garbage", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "garbage")
}

func TestEnsureProjectMarker_OptInDropsMarkerAtCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, PersistProjectMode(string(machineconfig.ModeOptIn), nil))

	build := filepath.Join(home, "some-project")
	require.NoError(t, os.MkdirAll(build, 0o755))
	t.Chdir(build)

	EnsureProjectMarker(nil)

	_, err := os.Stat(filepath.Join(build, paths.ProjectMarkerFilename))
	require.NoError(t, err)
}

func TestEnsureProjectMarker_AlwaysDoesNotDropMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	build := filepath.Join(home, "any-project")
	require.NoError(t, os.MkdirAll(build, 0o755))
	t.Chdir(build)

	EnsureProjectMarker(nil)

	_, err := os.Stat(filepath.Join(build, paths.ProjectMarkerFilename))
	assert.True(t, os.IsNotExist(err))
}

func TestEnsureProjectMarker_OptInAncestorMarkerSkipsWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, PersistProjectMode(string(machineconfig.ModeOptIn), nil))

	parent := filepath.Join(home, "parent")
	child := filepath.Join(parent, "child")
	require.NoError(t, os.MkdirAll(child, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(parent, paths.ProjectMarkerFilename), []byte(`{}`), 0o644))
	t.Chdir(child)

	EnsureProjectMarker(nil)

	_, err := os.Stat(filepath.Join(child, paths.ProjectMarkerFilename))
	assert.True(t, os.IsNotExist(err), "child must not get a marker when ancestor already covers it")
}
