//go:build unit

package updater

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDaemonInstalled_falseWhenAbsent(t *testing.T) {
	assert.False(t, DaemonInstalled(t.TempDir()))
}

func TestDaemonInstalled_detectsLaunchAgent(t *testing.T) {
	home := t.TempDir()
	plistDir := filepath.Join(home, "Library", "LaunchAgents")
	require.NoError(t, os.MkdirAll(plistDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(plistDir, "io.bitrise.build-cache.xcelerate-proxy.plist"), []byte("<plist/>"), 0o644))

	assert.True(t, DaemonInstalled(home))
}

func TestDaemonInstalled_detectsSystemdUnit(t *testing.T) {
	home := t.TempDir()
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	require.NoError(t, os.MkdirAll(unitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(unitDir, "bitrise-build-cache-ccache-helper.service"), []byte("[Unit]\n"), 0o644))

	assert.True(t, DaemonInstalled(home))
}

func TestPrintDaemonRestartHint_mentionsRestartCommand(t *testing.T) {
	var buf bytes.Buffer
	PrintDaemonRestartHint(loggerWithBuffer(&buf))
	assert.Contains(t, buf.String(), "daemon restart")
}
