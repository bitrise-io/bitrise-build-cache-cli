//go:build unit

package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUp_launchd_startsAllInstalledServices(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)

	// Install first so plist files exist on disk.
	installRunner := &recordingRunner{}
	_, err := Install(context.Background(), LaunchdBackend{Runner: installRunner}, paths, DefaultServices(), "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)

	upRunner := &recordingRunner{}
	result, err := Up(context.Background(), LaunchdBackend{Runner: upRunner}, paths, DefaultServices())
	require.NoError(t, err)
	require.Len(t, result.Statuses, 2)
	assert.Equal(t, "launchd", result.BackendName)

	// Up runs enable-then-bootout-then-bootstrap-then-kickstart per service on launchd.
	assert.Len(t, upRunner.calls, 8)
	assert.Equal(t, "enable", upRunner.calls[0][1])
	assert.Equal(t, "bootout", upRunner.calls[1][1])
	assert.Equal(t, "bootstrap", upRunner.calls[2][1])
	assert.Equal(t, "kickstart", upRunner.calls[3][1])
	assert.Equal(t, "enable", upRunner.calls[4][1])
	assert.Equal(t, "bootout", upRunner.calls[5][1])
	assert.Equal(t, "bootstrap", upRunner.calls[6][1])
	assert.Equal(t, "kickstart", upRunner.calls[7][1])
}

func TestUp_launchd_errorsWhenNotInstalled(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)
	runner := &recordingRunner{}

	_, err := Up(context.Background(), LaunchdBackend{Runner: runner}, paths, DefaultServices())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotInstalled), "expected ErrNotInstalled, got %v", err)
	assert.Empty(t, runner.calls, "Backend.Start must not be called when config is missing")
}

func TestDown_launchd_stopsWithoutRemovingConfig(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)

	// Install so plist files are on disk.
	installRunner := &recordingRunner{}
	_, err := Install(context.Background(), LaunchdBackend{Runner: installRunner}, paths, DefaultServices(), "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)

	downRunner := &recordingRunner{}
	_, err = Down(context.Background(), LaunchdBackend{Runner: downRunner}, paths, DefaultServices())
	require.NoError(t, err)

	require.Len(t, downRunner.calls, 2)
	assert.Equal(t, "bootout", downRunner.calls[0][1])
	assert.Equal(t, "bootout", downRunner.calls[1][1])

	// Plist files must remain on disk so Up can bring services back.
	for _, svc := range DefaultServices() {
		_, statErr := os.Stat(filepath.Join(paths.LaunchAgentsDir(), svc.Label()+".plist"))
		require.NoError(t, statErr, "plist file should still exist for %s", svc.Name)
	}
}

func TestDown_launchd_idempotentOnUnregistered(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)

	// Bootout reply: exit 5 = "service not loaded". Down must still succeed.
	runner := &recordingRunner{
		reply: func(_ string, _ []string) (string, string, int, error) {
			return "", "Could not find specified service", 5, nil
		},
	}

	_, err := Down(context.Background(), LaunchdBackend{Runner: runner}, paths, DefaultServices())
	require.NoError(t, err)
}

func TestRestart_launchd_callsDownThenUp(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)

	// Install first so config exists for the Up half of restart.
	installRunner := &recordingRunner{}
	_, err := Install(context.Background(), LaunchdBackend{Runner: installRunner}, paths, DefaultServices(), "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)

	restartRunner := &recordingRunner{}
	_, err = Restart(context.Background(), LaunchdBackend{Runner: restartRunner}, paths, DefaultServices())
	require.NoError(t, err)

	// Restart is Down (2 boots-out) + Up (2 * enable+bootout+bootstrap+kickstart) = 10 calls.
	require.Len(t, restartRunner.calls, 10)
	assert.Equal(t, "bootout", restartRunner.calls[0][1])
	assert.Equal(t, "bootout", restartRunner.calls[1][1])
	assert.Equal(t, "enable", restartRunner.calls[2][1])
	assert.Equal(t, "bootout", restartRunner.calls[3][1])
	assert.Equal(t, "bootstrap", restartRunner.calls[4][1])
	assert.Equal(t, "kickstart", restartRunner.calls[5][1])
	assert.Equal(t, "enable", restartRunner.calls[6][1])
	assert.Equal(t, "bootout", restartRunner.calls[7][1])
	assert.Equal(t, "bootstrap", restartRunner.calls[8][1])
	assert.Equal(t, "kickstart", restartRunner.calls[9][1])
}

func TestUp_systemd_startsAllInstalledServices(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)

	installRunner := &recordingRunner{}
	_, err := Install(context.Background(), SystemdBackend{Runner: installRunner}, paths, DefaultServices(), "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)

	upRunner := &recordingRunner{}
	result, err := Up(context.Background(), SystemdBackend{Runner: upRunner}, paths, DefaultServices())
	require.NoError(t, err)
	require.Len(t, result.Statuses, 2)
	assert.Equal(t, "systemd", result.BackendName)

	require.Len(t, upRunner.calls, 4)
	assert.Equal(t, "daemon-reload", upRunner.calls[0][2])
	assert.Equal(t, "enable", upRunner.calls[1][2])
}

func TestUp_systemd_errorsWhenNotInstalled(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)
	runner := &recordingRunner{}

	_, err := Up(context.Background(), SystemdBackend{Runner: runner}, paths, DefaultServices())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotInstalled))
	assert.Empty(t, runner.calls)
}

func TestDown_systemd_stopsButKeepsUnitFile(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)

	installRunner := &recordingRunner{}
	_, err := Install(context.Background(), SystemdBackend{Runner: installRunner}, paths, DefaultServices(), "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)

	downRunner := &recordingRunner{}
	_, err = Down(context.Background(), SystemdBackend{Runner: downRunner}, paths, DefaultServices())
	require.NoError(t, err)

	require.Len(t, downRunner.calls, 2)
	assert.Equal(t, "stop", downRunner.calls[0][2])

	// Unit files must remain so Up can re-enable them.
	for _, svc := range DefaultServices() {
		_, statErr := os.Stat(paths.UnitPath(svc.UnitName()))
		require.NoError(t, statErr, "unit file should still exist for %s", svc.Name)
	}
}

func TestDown_systemd_treatsNotLoadedAsSuccess(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)

	runner := &recordingRunner{
		reply: func(_ string, _ []string) (string, string, int, error) {
			return "", "Failed to stop bitrise-build-cache-xcelerate-proxy.service: Unit bitrise-build-cache-xcelerate-proxy.service not loaded.", 5, nil
		},
	}

	_, err := Down(context.Background(), SystemdBackend{Runner: runner}, paths, DefaultServices())
	require.NoError(t, err)
}

func TestRestart_systemd_callsDownThenUp(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)

	// Install first so the unit files exist (Up half of restart requires them).
	installRunner := &recordingRunner{}
	_, err := Install(context.Background(), SystemdBackend{Runner: installRunner}, paths, DefaultServices(), "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)

	restartRunner := &recordingRunner{}
	_, err = Restart(context.Background(), SystemdBackend{Runner: restartRunner}, paths, DefaultServices())
	require.NoError(t, err)

	require.Len(t, restartRunner.calls, 6)
	assert.Equal(t, "stop", restartRunner.calls[0][2])
	assert.Equal(t, "stop", restartRunner.calls[1][2])
	assert.Equal(t, "daemon-reload", restartRunner.calls[2][2])
	assert.Equal(t, "enable", restartRunner.calls[3][2])
	assert.Equal(t, "daemon-reload", restartRunner.calls[4][2])
	assert.Equal(t, "enable", restartRunner.calls[5][2])
}

// TestRestart_systemd_wrapsUpFailureWithStoppedHint locks the partial-
// failure error wrap: when Down succeeds but Up fails (e.g. because
// systemd is unreachable on the second call), the user is left with
// stopped services, so the error message MUST surface that state and
// the next remediation step.
func TestRestart_systemd_wrapsUpFailureWithStoppedHint(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)

	// Install so the Up half doesn't trip ErrNotInstalled (we want the
	// failure to come from the runner, not config-presence checks).
	installRunner := &recordingRunner{}
	_, err := Install(context.Background(), SystemdBackend{Runner: installRunner}, paths, DefaultServices(), "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)

	// stop succeeds, daemon-reload (Up's first call) fails.
	runner := &recordingRunner{
		reply: func(_ string, args []string) (string, string, int, error) {
			if len(args) > 1 && args[1] == "daemon-reload" {
				return "", "Failed to connect to bus: No such file or directory", 1, nil
			}

			return "", "", 0, nil
		},
	}

	_, err = Restart(context.Background(), SystemdBackend{Runner: runner}, paths, DefaultServices())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stopped")
	assert.Contains(t, err.Error(), "daemon up")
}
