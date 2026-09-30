//go:build unit

package xcode_app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The public Activator behaviour on non-darwin hosts short-circuits to
// ErrUnsupportedPlatform without touching the filesystem or launchctl.
// The darwin-only happy path is covered by manual verification and the
// internal-package tests for each collaborator.

func newLogger() log.Logger {
	return log.NewLogger(log.WithOutput(&bytes.Buffer{}))
}

func TestEnable_returnsErrUnsupportedPlatformOnNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-darwin-only assertion")
	}

	a := &Activator{Logger: newLogger()}

	_, err := a.Enable(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedPlatform)
}

func TestDisable_returnsErrUnsupportedPlatformOnNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-darwin-only assertion")
	}

	a := &Activator{Logger: newLogger()}

	_, err := a.Disable(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedPlatform)
}

func TestEnable_missingXcelerateConfigReturnsSentinel(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only: this path runs after the platform check")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	// Ensure no config on disk at the xcelerate default path.
	require.NoDirExists(t, filepath.Join(home, ".bitrise-xcelerate"))

	a := &Activator{Logger: newLogger(), Envs: map[string]string{}}

	_, err := a.Enable(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrXcelerateNotConfigured)
}

func TestEnable_emptyProxySocketIsError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only: this path runs after the platform check")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeXcelerateConfig(t, home, `{"proxySocketPath":""}`)

	a := &Activator{Logger: newLogger(), Envs: map[string]string{}}

	_, err := a.Enable(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty proxy socket path")
}

func writeXcelerateConfig(t *testing.T, home, body string) {
	t.Helper()

	dir := filepath.Join(home, ".bitrise-xcelerate")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644))
}
