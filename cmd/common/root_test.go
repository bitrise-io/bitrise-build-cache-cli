//go:build unit

package common

import (
	"io"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShouldSkipVersionCheck_authTokenIsSkipped is the regression guard for
// ACI-5125: the Gradle init script's BitriseAuthTokenSource captures the
// stdout of `bitrise-build-cache auth token`. If `token` is not in the skip
// list the version-drift nudge fires whenever cooldown is up — and on top
// of the stderr-routing fix below, the GitHub-API round-trip is wasted on
// every gradle build configuration phase.
func TestShouldSkipVersionCheck_authTokenIsSkipped(t *testing.T) {
	cmd := &cobra.Command{Use: "token"}
	assert.True(t, ShouldSkipVersionCheck(cmd),
		"the `auth token` subcommand must skip the version check entirely")
}

// TestNewVersionCheckLogger_writesToStderr is the regression guard for the
// other half of ACI-5125: the go-utils logger writes to os.Stdout by default,
// which routed the version-drift nudge onto the same stream Gradle reads the
// token from. The configured logger must target stderr.
func TestNewVersionCheckLogger_writesToStderr(t *testing.T) {
	// Capture os.Stderr.
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w

	defer func() { os.Stderr = origStderr }()

	logger := newVersionCheckLogger()
	logger.Warnf("test-nudge")

	require.NoError(t, w.Close())
	got, err := io.ReadAll(r)
	require.NoError(t, err)

	assert.Contains(t, string(got), "test-nudge",
		"the version-check logger must write to os.Stderr, not stdout")
}
