//go:build unit

package xcode_app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// On non-darwin hosts Enable / Disable short-circuit to the sentinel without
// touching the filesystem or launchctl. We can only validate this branch on
// a non-darwin runner; the happy path is exercised through the internal
// package tests + manual macOS verification + the daemon-install-macos e2e.

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

func TestResolvePreviousOverride_passesThroughRealPriorPath(t *testing.T) {
	got := resolvePreviousOverride(newLogger(),"/Users/me/Base.xcconfig", "/tmp/state.json", "/tmp/xcode-app.xcconfig")
	assert.Equal(t, "/Users/me/Base.xcconfig", got)
}

func TestResolvePreviousOverride_selfLoopReadsStoredState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	raw, err := json.Marshal(map[string]string{"previousXCConfigPath": "/Users/me/Base.xcconfig"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(statePath, raw, 0o600))

	got := resolvePreviousOverride(newLogger(),"/tmp/xcode-app.xcconfig", statePath, "/tmp/xcode-app.xcconfig")
	assert.Equal(t, "/Users/me/Base.xcconfig", got)
}

func TestResolvePreviousOverride_selfLoopWithMissingStateReturnsEmpty(t *testing.T) {
	got := resolvePreviousOverride(newLogger(),"/tmp/xcode-app.xcconfig", filepath.Join(t.TempDir(), "no-such.json"), "/tmp/xcode-app.xcconfig")
	assert.Empty(t, got)
}

func TestResolvePreviousOverride_selfLoopWithCorruptStateReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	require.NoError(t, os.WriteFile(statePath, []byte("{not-json"), 0o600))

	got := resolvePreviousOverride(newLogger(),"/tmp/xcode-app.xcconfig", statePath, "/tmp/xcode-app.xcconfig")
	assert.Empty(t, got)
}
