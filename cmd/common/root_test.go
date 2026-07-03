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

func TestShouldSkipVersionCheck_authTokenIsSkipped(t *testing.T) {
	cmd := &cobra.Command{Use: "token"}
	assert.True(t, ShouldSkipVersionCheck(cmd))
}

func TestNewVersionCheckLogger_writesToStderr(t *testing.T) {
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

	assert.Contains(t, string(got), "test-nudge")
}
