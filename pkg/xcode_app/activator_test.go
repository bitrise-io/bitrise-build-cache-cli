//go:build unit

package xcode_app

import (
	"bytes"
	"context"
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
