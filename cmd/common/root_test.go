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

func TestSkipLists_DivergeOnStatusAndToken(t *testing.T) {
	status := &cobra.Command{Use: "status"}
	assert.True(t, shouldSkipHydrate(status))
	assert.False(t, ShouldSkipVersionCheck(status))

	token := &cobra.Command{Use: "token"}
	assert.False(t, shouldSkipHydrate(token))
	assert.True(t, ShouldSkipVersionCheck(token))
}

func TestShouldSkipHydrate(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"xcodebuild", true},
		{"login", true},
		{"activate", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: tc.name}
			assert.Equal(t, tc.want, shouldSkipHydrate(cmd))
		})
	}
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
