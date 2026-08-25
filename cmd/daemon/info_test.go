//go:build unit

package daemon

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/ccache/protocol"
	daemonpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/daemon"
)

func TestProbeStatus_mapsProbeToHumanString(t *testing.T) {
	assert.Equal(t, statusRunning, probeStatus(daemonpkg.ProbeRunning))
	assert.Equal(t, statusStuck, probeStatus(daemonpkg.ProbeStuck))
	assert.Equal(t, statusStopped, probeStatus(daemonpkg.ProbeStopped))
}

func TestProbeSocket_missingFile(t *testing.T) {
	assert.Equal(t, daemonpkg.ProbeStopped, daemonpkg.ProbeSocket(t.Context(), filepath.Join(t.TempDir(), "does-not-exist.sock")))
}

func TestProbeSocket_fileExistsButNothingListening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale.sock")
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	assert.Equal(t, daemonpkg.ProbeStuck, daemonpkg.ProbeSocket(t.Context(), path))
}

func TestProbeSocket_listeningSocket(t *testing.T) {
	// Unix socket paths max out at ~104 chars on darwin; use a short dir under /tmp.
	dir, err := os.MkdirTemp("/tmp", "probe-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "live.sock")

	l, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	assert.Equal(t, daemonpkg.ProbeRunning, daemonpkg.ProbeSocket(t.Context(), path))
}

func TestProbeCcacheSocket_missingFile(t *testing.T) {
	assert.Equal(t, daemonpkg.ProbeStopped, daemonpkg.ProbeCcacheSocket(t.Context(), filepath.Join(t.TempDir(), "does-not-exist.sock")))
}

func TestProbeCcacheSocket_fileExistsButNothingListening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale.sock")
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	assert.Equal(t, daemonpkg.ProbeStuck, daemonpkg.ProbeCcacheSocket(t.Context(), path))
}

func TestProbeCcacheSocket_healthCheckOK(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "probe-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "ccache.sock")

	l, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	go func() {
		conn, aerr := l.Accept()
		if aerr != nil {
			return
		}
		defer conn.Close()
		if err := protocol.WriteGreeting(conn); err != nil {
			return
		}
		req, err := protocol.ReadByte(conn)
		if err != nil || req != protocol.RequestHealthCheck {
			return
		}
		_ = protocol.WriteByte(conn, protocol.ResponseOK)
	}()

	assert.Equal(t, daemonpkg.ProbeRunning, daemonpkg.ProbeCcacheSocket(t.Context(), path))
}

func TestProbeSocket_acceptButHangIsReportedRunning(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "probe-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "slow.sock")

	l, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	// accept-and-hang: server accepts the connection but never reads/writes.
	// probeSocket is dial-only, so it should still see this as running.
	go func() {
		conn, aerr := l.Accept()
		if aerr != nil {
			return
		}
		<-t.Context().Done()
		_ = conn.Close()
	}()

	assert.Equal(t, daemonpkg.ProbeRunning, daemonpkg.ProbeSocket(t.Context(), path))
}
