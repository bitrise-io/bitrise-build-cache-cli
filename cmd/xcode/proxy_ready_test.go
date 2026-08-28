//go:build unit

package xcode

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// t.TempDir() embeds the test name, which overruns the 104-byte sun_path limit on
// macOS — unix sockets need a short directory.
func shortTempDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "px")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	return dir
}

// A proxy that answers on its socket is ready immediately.
func TestAwaitProxySocket_ServingSocketIsReady(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "proxy.sock")
	l, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	assert.True(t, awaitProxySocket(context.Background(), path, time.Second))
}

// The regression this guards: a proxy holding the singleton without serving must
// not be reported ready, or every cache operation of the build fails silently.
func TestAwaitProxySocket_AbsentSocketTimesOut(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "missing.sock")

	start := time.Now()
	assert.False(t, awaitProxySocket(context.Background(), path, 300*time.Millisecond))
	assert.Less(t, time.Since(start), 5*time.Second, "must give up at the timeout, not hang the build")
}

// A socket that appears late still counts: a supervised proxy is revived by
// launchd/systemd rather than by the wrapper.
func TestAwaitProxySocket_WaitsForALateSocket(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "late.sock")

	listening := make(chan net.Listener, 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		if l, err := net.Listen("unix", path); err == nil {
			listening <- l
		}
	}()
	t.Cleanup(func() {
		select {
		case l := <-listening:
			_ = l.Close()
		default:
		}
	})

	assert.True(t, awaitProxySocket(context.Background(), path, 5*time.Second))
}

// A cancelled build must not keep polling.
func TestAwaitProxySocket_HonoursContextCancellation(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "missing.sock")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.False(t, awaitProxySocket(ctx, path, 10*time.Second))
}
