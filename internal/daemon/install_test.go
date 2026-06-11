//go:build unit

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingRunner captures every launchctl invocation for assertion. Optional
// reply hooks let individual tests force exit codes (e.g. simulate "service
// not loaded").
type recordingRunner struct {
	mu    sync.Mutex
	calls [][]string
	reply func(args []string) (string, string, int, error)
}

func (r *recordingRunner) Run(_ context.Context, args ...string) (string, string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := append([]string(nil), args...)
	r.calls = append(r.calls, cp)

	if r.reply != nil {
		return r.reply(cp)
	}

	return "", "", 0, nil
}

func TestInstall_writesPlistsAndBootstraps(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("install path is darwin-only")
	}

	home := t.TempDir()
	paths := NewPathsFromHome(home)
	runner := &recordingRunner{}

	result, err := Install(context.Background(), runner, paths, DefaultServices(), "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)
	require.Len(t, result.Statuses, 2)

	for _, st := range result.Statuses {
		assert.True(t, st.Wrote, "service %s should be marked written", st.Service.Name)
		_, statErr := os.Stat(st.PlistPath)
		require.NoError(t, statErr, "plist file should exist on disk: %s", st.PlistPath)
		assert.True(t, filepath.IsAbs(st.PlistPath))
	}

	// Every install does bootout-then-bootstrap per service: 2 services * 2 calls.
	assert.Len(t, runner.calls, 4)
	assert.Equal(t, "bootout", runner.calls[0][0])
	assert.Equal(t, "bootstrap", runner.calls[1][0])
	assert.Equal(t, "bootout", runner.calls[2][0])
	assert.Equal(t, "bootstrap", runner.calls[3][0])
}

func TestInstall_idempotent_secondRunOverwritesPlist(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("install path is darwin-only")
	}

	home := t.TempDir()
	paths := NewPathsFromHome(home)
	runner := &recordingRunner{}

	_, err := Install(context.Background(), runner, paths, DefaultServices(), "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)

	// Rerun with a different binary path — simulates a CLI upgrade.
	_, err = Install(context.Background(), runner, paths, DefaultServices(), "/opt/new/bitrise-build-cache")
	require.NoError(t, err)

	plistPath := paths.PlistPath(DefaultServices()[0].Label())
	body, err := os.ReadFile(plistPath) //nolint:gosec // test path under t.TempDir()
	require.NoError(t, err)
	assert.Contains(t, string(body), "/opt/new/bitrise-build-cache")
	assert.NotContains(t, string(body), "/usr/local/bin/bitrise-build-cache")
}

func TestUninstall_removesPlistsAndTolerantOfMissingService(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("install path is darwin-only")
	}

	home := t.TempDir()
	paths := NewPathsFromHome(home)

	// First install so plists exist on disk.
	installRunner := &recordingRunner{}
	_, err := Install(context.Background(), installRunner, paths, DefaultServices(), "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)

	// Bootout reply: exit code 5 simulates "service not loaded" — must still
	// be treated as success.
	uninstallRunner := &recordingRunner{
		reply: func(_ []string) (string, string, int, error) { return "", "Could not find specified service", 5, nil },
	}

	result, err := Uninstall(context.Background(), uninstallRunner, paths, DefaultServices())
	require.NoError(t, err)
	require.Len(t, result.Statuses, 2)

	for _, st := range result.Statuses {
		assert.True(t, st.Removed, "service %s should be marked removed", st.Service.Name)
		_, statErr := os.Stat(st.PlistPath)
		assert.True(t, os.IsNotExist(statErr), "plist file should be gone: %s", st.PlistPath)
	}
}

func TestUninstall_missingPlistFileIsNoError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("install path is darwin-only")
	}

	home := t.TempDir()
	paths := NewPathsFromHome(home)

	runner := &recordingRunner{}
	result, err := Uninstall(context.Background(), runner, paths, DefaultServices())
	require.NoError(t, err)

	for _, st := range result.Statuses {
		assert.False(t, st.Removed, "no plist existed, so nothing to remove")
	}
}
