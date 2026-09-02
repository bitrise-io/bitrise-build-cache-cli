//go:build unit

package ccache

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/spawn"
)

func probeTempDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "pb")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	return dir
}

func TestProbeSocket_ReportsStoppedWhenNoSocketFile(t *testing.T) {
	assert.Equal(t, spawn.Stopped,
		ProbeSocket(context.Background(), filepath.Join(probeTempDir(t), "missing.sock")))
}

// A crashed helper leaves its socket file behind. A stat-only check would call
// it healthy and every ccache lookup would miss for the whole build.
func TestProbeSocket_ReportsStuckForAStaleSocketFile(t *testing.T) {
	path := filepath.Join(probeTempDir(t), "stale.sock")
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	assert.Equal(t, spawn.Stuck, ProbeSocket(context.Background(), path))
}

// Accepting a connection is not the same as serving: the handshake is what
// separates a live helper from a listener that never answers.
func TestProbeSocket_ReportsStuckForAListenerThatNeverAnswers(t *testing.T) {
	path := filepath.Join(probeTempDir(t), "mute.sock")
	l, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	assert.Equal(t, spawn.Stuck, ProbeSocket(context.Background(), path))
}

func TestAwaitSocket_GivesUpAtTheBudget(t *testing.T) {
	path := filepath.Join(probeTempDir(t), "missing.sock")

	start := time.Now()
	assert.False(t, AwaitSocket(context.Background(), path, 200*time.Millisecond, 50*time.Millisecond))
	assert.Less(t, time.Since(start), 3*time.Second, "must give up at the budget, not hang the build")
}

func TestAwaitSocket_HonoursContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.False(t, AwaitSocket(ctx, filepath.Join(probeTempDir(t), "missing.sock"), time.Minute, 50*time.Millisecond))
}
