//go:build unit

package refresh

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_missingReturnsEmpty(t *testing.T) {
	home := t.TempDir()

	reg, err := Load(home)
	require.NoError(t, err)
	assert.Empty(t, reg.Entries)
}

func TestSaveAndLoad_roundTrip(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 6, 11, 15, 4, 5, 0, time.UTC)

	want := Registry{Entries: map[string]Entry{
		ToolGradle: {Tool: ToolGradle, ConfigPath: "/home/u/.gradle/init.d/x.kts", CLIVersion: "2.8.4", RegisteredAt: now},
		ToolBazel:  {Tool: ToolBazel, ConfigPath: "/home/u/.bazelrc", CLIVersion: "2.8.4", RegisteredAt: now},
	}}

	require.NoError(t, Save(home, want))

	got, err := Load(home)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestMark_overwritesExistingEntry(t *testing.T) {
	home := t.TempDir()

	require.NoError(t, Mark(home, ToolGradle, "/p1", "2.8.4"))
	first, _ := Load(home)

	// Sleep just enough to make sure RegisteredAt advances on the second
	// mark (we don't care about the exact delta; only that it moves).
	time.Sleep(2 * time.Millisecond)

	require.NoError(t, Mark(home, ToolGradle, "/p2", "2.8.5"))
	second, _ := Load(home)

	assert.Equal(t, "/p2", second.Entries[ToolGradle].ConfigPath)
	assert.Equal(t, "2.8.5", second.Entries[ToolGradle].CLIVersion)
	assert.True(t, second.Entries[ToolGradle].RegisteredAt.After(first.Entries[ToolGradle].RegisteredAt))
}

func TestMark_preservesEntriesForOtherTools(t *testing.T) {
	home := t.TempDir()

	require.NoError(t, Mark(home, ToolGradle, "/g", "2.8.4"))
	require.NoError(t, Mark(home, ToolBazel, "/b", "2.8.4"))

	reg, err := Load(home)
	require.NoError(t, err)
	assert.Len(t, reg.Entries, 2)
}

func TestSortedEntries_alphabetical(t *testing.T) {
	reg := Registry{Entries: map[string]Entry{
		ToolXcelerate: {Tool: ToolXcelerate},
		ToolGradle:    {Tool: ToolGradle},
		ToolCcache:    {Tool: ToolCcache},
		ToolBazel:     {Tool: ToolBazel},
	}}

	got := reg.SortedEntries()
	require.Len(t, got, 4)
	assert.Equal(t, ToolBazel, got[0].Tool)
	assert.Equal(t, ToolCcache, got[1].Tool)
	assert.Equal(t, ToolGradle, got[2].Tool)
	assert.Equal(t, ToolXcelerate, got[3].Tool)
}

func TestMark_parallelMarksAllSurvive(t *testing.T) {
	home := t.TempDir()

	// Run Mark for every tool in parallel; without the flock guard one of
	// the entries would be lost on Save because of the read-modify-write
	// race (each goroutine Loads the same baseline, writes its own delta,
	// rename last-wins).
	tools := []string{ToolGradle, ToolBazel, ToolXcelerate, ToolCcache}

	done := make(chan error, len(tools))
	for _, tool := range tools {
		go func(tn string) {
			done <- Mark(home, tn, "/path/"+tn, "2.8.4")
		}(tool)
	}

	for range tools {
		require.NoError(t, <-done)
	}

	reg, err := Load(home)
	require.NoError(t, err)
	assert.Len(t, reg.Entries, len(tools), "every Mark call must be visible in the final registry")

	for _, tool := range tools {
		assert.Contains(t, reg.Entries, tool)
	}
}

func TestLoad_corruptFileErrors(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, StateDirRelative)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, RegistryFile), []byte("not json"), 0o644))

	_, err := Load(home)
	require.Error(t, err)
}
