//go:build unit

package versioncheck

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadState_missingFileReturnsZero(t *testing.T) {
	home := t.TempDir()

	st, err := LoadState(home)
	require.NoError(t, err)
	assert.Equal(t, State{}, st)
}

func TestSaveAndLoadState_roundTrip(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 6, 11, 15, 4, 5, 0, time.UTC)

	want := State{
		LastVersion: "2.8.4",
		LastSeenAt:  now,
		LastNudgeAt: now.Add(-2 * time.Hour),
	}

	require.NoError(t, SaveState(home, want))

	got, err := LoadState(home)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestSaveState_createsStateDir(t *testing.T) {
	home := t.TempDir()

	require.NoError(t, SaveState(home, State{LastVersion: "2.8.4", LastSeenAt: time.Now()}))

	info, err := os.Stat(filepath.Join(home, StateDirRelative))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestLoadState_corruptFileErrors(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, StateDirRelative)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, StateFile), []byte("not json"), 0o644))

	_, err := LoadState(home)
	require.Error(t, err)
}
