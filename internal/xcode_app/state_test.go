//go:build unit

package xcode_app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadState_missingFileReturnsZeroValueAndFalse(t *testing.T) {
	got, found, err := LoadState(filepath.Join(t.TempDir(), "nope.json"))
	require.NoError(t, err)
	assert.False(t, found)
	assert.Empty(t, got.PreviousXCConfigPath)
}

func TestSaveLoadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	want := State{PreviousXCConfigPath: "/Users/me/Base.xcconfig"}
	require.NoError(t, SaveState(path, want))

	got, found, err := LoadState(path)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, want, got)
}

func TestSaveState_atomicRename_noTmpLeftBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	require.NoError(t, SaveState(path, State{PreviousXCConfigPath: "/x"}))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "no .tmp leftover expected")
	}
}

func TestRemoveState_missingFileIsNotAnError(t *testing.T) {
	require.NoError(t, RemoveState(filepath.Join(t.TempDir(), "nope.json")))
}
