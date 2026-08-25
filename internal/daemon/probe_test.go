//go:build unit

package daemon

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeSocket_missing(t *testing.T) {
	assert.Equal(t, ProbeStopped, ProbeSocket(t.Context(), filepath.Join(t.TempDir(), "does-not-exist.sock")))
}

func TestProbeSocket_stale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale.sock")
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	assert.Equal(t, ProbeStuck, ProbeSocket(t.Context(), path))
}

func TestProbeSocket_live(t *testing.T) {
	// Unix socket path is capped at ~104 chars on darwin; use a short dir under /tmp.
	dir, err := os.MkdirTemp("/tmp", "probe-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	path := filepath.Join(dir, "live.sock")
	l, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	assert.Equal(t, ProbeRunning, ProbeSocket(t.Context(), path))
}

func TestProbeCcacheSocket_missing(t *testing.T) {
	assert.Equal(t, ProbeStopped, ProbeCcacheSocket(t.Context(), filepath.Join(t.TempDir(), "does-not-exist.sock")))
}

func TestProbeCcacheSocket_stale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale.sock")
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	assert.Equal(t, ProbeStuck, ProbeCcacheSocket(t.Context(), path))
}

func TestProbeSocket_ctxCanceledIsStuck(t *testing.T) {
	// A canceled context can only manifest during the dial (post-stat). Create
	// a fresh socket file so stat succeeds, then pass a canceled ctx — the
	// dial fails and we should get Stuck.
	path := filepath.Join(t.TempDir(), "canceled.sock")
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.Equal(t, ProbeStuck, ProbeSocket(ctx, path))
}
