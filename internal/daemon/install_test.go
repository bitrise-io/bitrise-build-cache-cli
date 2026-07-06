//go:build unit

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingRunner captures every supervisor-CLI invocation for assertion.
// Optional reply hook lets individual tests force exit codes (e.g. simulate
// "service not loaded").
type recordingRunner struct {
	mu    sync.Mutex
	calls [][]string
	reply func(bin string, args []string) (string, string, int, error)
}

func (r *recordingRunner) Run(_ context.Context, bin string, args ...string) (string, string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cp := append([]string{bin}, args...)
	r.calls = append(r.calls, cp)

	if r.reply != nil {
		return r.reply(bin, args)
	}

	return "", "", 0, nil
}

func TestInstall_launchd_writesPlistsAndBootstraps(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)
	runner := &recordingRunner{}
	backend := LaunchdBackend{Runner: runner}

	result, err := Install(context.Background(), backend, paths, DefaultServices(), "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)
	require.Len(t, result.Statuses, 2)
	assert.Equal(t, "launchd", result.BackendName)

	for _, st := range result.Statuses {
		assert.True(t, st.Wrote, "service %s should be marked written", st.Service.Name)
		_, statErr := os.Stat(st.ConfigPath)
		require.NoError(t, statErr, "plist file should exist on disk: %s", st.ConfigPath)
		assert.True(t, filepath.IsAbs(st.ConfigPath))
	}

	// Every install does bootout-then-bootstrap-then-kickstart per service: 2 services * 3 calls.
	// kickstart -k is required to actually start the process on macOS Sequoia; bootstrap alone
	// doesn't reliably honour RunAtLoad. See ACI-5177.
	assert.Len(t, runner.calls, 6)
	// calls[*][0] = launchctl bin, calls[*][1] = subcommand.
	assert.Equal(t, "bootout", runner.calls[0][1])
	assert.Equal(t, "bootstrap", runner.calls[1][1])
	assert.Equal(t, "kickstart", runner.calls[2][1])
	assert.Equal(t, "-k", runner.calls[2][2])
	assert.Equal(t, "bootout", runner.calls[3][1])
	assert.Equal(t, "bootstrap", runner.calls[4][1])
	assert.Equal(t, "kickstart", runner.calls[5][1])
	assert.Equal(t, "-k", runner.calls[5][2])
}

func TestInstall_launchd_idempotent_secondRunOverwritesPlist(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)
	runner := &recordingRunner{}
	backend := LaunchdBackend{Runner: runner}

	_, err := Install(context.Background(), backend, paths, DefaultServices(), "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)

	// Rerun with a different binary path — simulates a CLI upgrade.
	_, err = Install(context.Background(), backend, paths, DefaultServices(), "/opt/new/bitrise-build-cache")
	require.NoError(t, err)

	plistPath := paths.PlistPath(DefaultServices()[0].Label())
	body, err := os.ReadFile(plistPath) //nolint:gosec // test path under t.TempDir()
	require.NoError(t, err)
	assert.Contains(t, string(body), "/opt/new/bitrise-build-cache")
	assert.NotContains(t, string(body), "/usr/local/bin/bitrise-build-cache")
}

func TestUninstall_launchd_removesPlistsAndTolerantOfMissingService(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)

	// First install so plists exist on disk.
	installRunner := &recordingRunner{}
	installBackend := LaunchdBackend{Runner: installRunner}
	_, err := Install(context.Background(), installBackend, paths, DefaultServices(), "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)

	// Bootout reply: exit code 5 simulates "service not loaded" — must still
	// be treated as success.
	uninstallRunner := &recordingRunner{
		reply: func(_ string, _ []string) (string, string, int, error) {
			return "", "Could not find specified service", 5, nil
		},
	}
	uninstallBackend := LaunchdBackend{Runner: uninstallRunner}

	result, err := Uninstall(context.Background(), uninstallBackend, paths, DefaultServices())
	require.NoError(t, err)
	require.Len(t, result.Statuses, 2)

	for _, st := range result.Statuses {
		assert.True(t, st.Removed, "service %s should be marked removed", st.Service.Name)
		_, statErr := os.Stat(st.ConfigPath)
		assert.True(t, os.IsNotExist(statErr), "plist file should be gone: %s", st.ConfigPath)
	}
}

func TestUninstall_launchd_missingPlistFileIsNoError(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)

	runner := &recordingRunner{}
	backend := LaunchdBackend{Runner: runner}

	result, err := Uninstall(context.Background(), backend, paths, DefaultServices())
	require.NoError(t, err)

	for _, st := range result.Statuses {
		assert.False(t, st.Removed, "no plist existed, so nothing to remove")
	}
}
