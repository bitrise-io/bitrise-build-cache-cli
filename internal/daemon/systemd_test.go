//go:build unit

package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstall_systemd_writesUnitsAndEnables(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)
	runner := &recordingRunner{}
	backend := SystemdBackend{Runner: runner}

	result, err := Install(context.Background(), backend, paths, DefaultServices(), "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)
	require.Len(t, result.Statuses, 2)
	assert.Equal(t, "systemd", result.BackendName)

	for _, st := range result.Statuses {
		assert.True(t, st.Wrote, "service %s should be marked written", st.Service.Name)
		_, statErr := os.Stat(st.ConfigPath)
		require.NoError(t, statErr, "unit file should exist on disk: %s", st.ConfigPath)
		assert.True(t, strings.HasSuffix(st.ConfigPath, ".service"))
		assert.True(t, filepath.IsAbs(st.ConfigPath))
	}

	// Per service: daemon-reload + enable --now = 2 calls. 2 services = 4 total.
	assert.Len(t, runner.calls, 4)

	// Each call's first element is the systemctl binary; subsequent elements
	// are --user + subcommand + ... — assert the subcommands cycle correctly.
	assert.Equal(t, "daemon-reload", runner.calls[0][2])
	assert.Equal(t, "enable", runner.calls[1][2])
	assert.Equal(t, "--now", runner.calls[1][3])
	assert.Equal(t, "daemon-reload", runner.calls[2][2])
	assert.Equal(t, "enable", runner.calls[3][2])
}

func TestUninstall_systemd_disablesAndRemovesUnit(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)

	// Install first so the unit file exists.
	installRunner := &recordingRunner{}
	_, err := Install(context.Background(), SystemdBackend{Runner: installRunner}, paths, DefaultServices(), "/usr/local/bin/bitrise-build-cache")
	require.NoError(t, err)

	uninstallRunner := &recordingRunner{}
	result, err := Uninstall(context.Background(), SystemdBackend{Runner: uninstallRunner}, paths, DefaultServices())
	require.NoError(t, err)
	require.Len(t, result.Statuses, 2)

	for _, st := range result.Statuses {
		assert.True(t, st.Removed, "service %s should be marked removed", st.Service.Name)
		_, statErr := os.Stat(st.ConfigPath)
		assert.True(t, os.IsNotExist(statErr), "unit file should be gone: %s", st.ConfigPath)
	}
}

func TestUninstall_systemd_treatsMissingUnitAsSuccess(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)

	// `systemctl --user disable --now` on a never-installed unit exits 1 with
	// "Unit file <name>.service does not exist." — we treat that as success.
	runner := &recordingRunner{
		reply: func(_ string, args []string) (string, string, int, error) {
			if len(args) > 1 && args[1] == "disable" {
				return "", "Failed to disable unit: Unit file bitrise-build-cache-xcelerate-proxy.service does not exist.", 1, nil
			}

			return "", "", 0, nil
		},
	}

	result, err := Uninstall(context.Background(), SystemdBackend{Runner: runner}, paths, DefaultServices())
	require.NoError(t, err)

	for _, st := range result.Statuses {
		assert.False(t, st.Removed, "no unit existed, so nothing to remove")
	}
}

func TestUninstall_systemd_propagatesUnexpectedDisableFailure(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)

	runner := &recordingRunner{
		reply: func(_ string, args []string) (string, string, int, error) {
			if len(args) > 1 && args[1] == "disable" {
				return "", "Failed to talk to manager: Connection refused", 1, nil
			}

			return "", "", 0, nil
		},
	}

	_, err := Uninstall(context.Background(), SystemdBackend{Runner: runner}, paths, DefaultServices())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Connection refused")
}

func TestUninstall_systemd_propagatesRunnerError(t *testing.T) {
	home := t.TempDir()
	paths := NewPathsFromHome(home)

	runner := &recordingRunner{
		reply: func(_ string, _ []string) (string, string, int, error) {
			return "", "", -1, errors.New("systemctl not found")
		},
	}

	_, err := Uninstall(context.Background(), SystemdBackend{Runner: runner}, paths, DefaultServices())
	require.Error(t, err)
}
