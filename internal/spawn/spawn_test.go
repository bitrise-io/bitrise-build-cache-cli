//go:build unit

package spawn

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// t.TempDir() embeds the test name, which overruns the 104-byte sun_path limit
// on macOS — unix sockets need a short directory.
func shortTempDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "sp")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	return dir
}

func TestServiceArgs(t *testing.T) {
	assert.Equal(t, []string{"xcelerate", "start-proxy"}, XcelerateProxy().Args)
	assert.Equal(t, []string{"ccache", "storage-helper", "start"}, CcacheHelper().Args)
}

// --debug is a persistent root flag, so it has to precede the subcommand or
// cobra rejects it.
func TestWithDebug_PrependsTheRootFlag(t *testing.T) {
	assert.Equal(t, []string{"--debug", "xcelerate", "start-proxy"}, XcelerateProxy().WithDebug().Args)
}

func TestWithArgs_DoesNotMutateTheReceiver(t *testing.T) {
	base := CcacheHelper()
	extended := base.WithArgs("--invocation-id=abc")

	assert.Equal(t, []string{"ccache", "storage-helper", "start"}, base.Args)
	assert.Equal(t, []string{"ccache", "storage-helper", "start", "--invocation-id=abc"}, extended.Args)
}

func TestProbe_ReportsStoppedWhenNoSocketFile(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "missing.sock")

	assert.Equal(t, Stopped, Probe(context.Background(), path))
}

func TestProbe_ReportsRunningForAServingSocket(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "live.sock")
	l, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	assert.Equal(t, Running, Probe(context.Background(), path))
}

// A leftover socket file after a crash is the case that matters: it exists, so
// a stat-only check would call the service healthy while every cache operation
// fails.
func TestProbe_ReportsStuckForAnUnservedSocketFile(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "stale.sock")
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	assert.Equal(t, Stuck, Probe(context.Background(), path))
}

func TestAwaitSocket_WaitsForALateSocket(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "late.sock")

	listening := make(chan net.Listener, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
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

	assert.True(t, AwaitSocket(context.Background(), path, 3*time.Second, 50*time.Millisecond))
}

func TestAwaitSocket_GivesUpAtTheBudget(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "missing.sock")

	start := time.Now()
	assert.False(t, AwaitSocket(context.Background(), path, 300*time.Millisecond, 50*time.Millisecond))
	assert.Less(t, time.Since(start), 3*time.Second, "must give up at the budget, not hang the build")
}

func TestAwaitSocket_HonoursContextCancellation(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "missing.sock")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.False(t, AwaitSocket(ctx, path, time.Minute, 50*time.Millisecond))
}

func TestRemoveLegacySupervision_NoopWhenNothingWasInstalled(t *testing.T) {
	assert.False(t, RemoveLegacySupervision(context.Background(), paths.FromHome(t.TempDir()), XcelerateProxy()))
}

// A CLI at or below v3.6.9 left these behind. They keep restarting a supervised
// service, so activation and the wrapper have to be able to retire them.
func TestRemoveLegacySupervision_DeletesALeftoverConfig(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("launchd-only: the linux arm shells out to systemctl")
	}

	home := t.TempDir()
	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.LaunchAgentsDir(), 0o755))

	plist := p.PlistPath(legacyLabelPrefix + NameXcelerateProxy)
	require.NoError(t, os.WriteFile(plist, []byte("<plist/>"), 0o644))

	assert.True(t, RemoveLegacySupervision(context.Background(), p, XcelerateProxy()))
	assert.NoFileExists(t, plist)
}

// This is the guard, not a quirk: Detached re-execs os.Executable(), which
// under `go test` is the test binary. Without the refusal a single call reruns
// the package, which calls again — the fork bomb that froze a laptop while this
// branch was being written.
func TestDetached_RefusesToReExecTheTestBinary(t *testing.T) {
	pid, err := Detached(XcelerateProxy())

	require.ErrorIs(t, err, ErrUnderTest)
	assert.Zero(t, pid)
}

// A crashed service leaves its socket file behind, so the stat cannot tell it
// from a live one — the handshake is what does. Without this the service would
// be reported healthy while every cache operation failed for the whole build.
func TestProbeWith_ConsultsTheHandshakeForAStaleSocketFile(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "stale.sock")
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	called := false
	state := ProbeWith(context.Background(), path, func(context.Context, string) error {
		called = true

		return assert.AnError
	})

	assert.True(t, called, "a socket file that exists still has to be handshaked")
	assert.Equal(t, Stuck, state)
}

// Accepting a connection is not the same as answering: the handshake is the
// only thing that separates a live service from a listener that never replies.
func TestProbeWith_ReportsStuckWhenTheHandshakeFails(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "mute.sock")
	l, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	state := ProbeWith(context.Background(), path, func(context.Context, string) error {
		return assert.AnError
	})

	assert.Equal(t, Stuck, state)
}

func TestProbeWith_ReportsRunningWhenTheHandshakeSucceeds(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "live.sock")
	l, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	state := ProbeWith(context.Background(), path, func(context.Context, string) error { return nil })

	assert.Equal(t, Running, state)
}

// The handshake has to be honoured on every poll, or a service that only comes
// up late is reported ready as soon as its socket file appears.
func TestAwaitSocketWith_UsesTheHandshake(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "late.sock")
	l, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	calls := 0
	ok := AwaitSocketWith(context.Background(), path, func(context.Context, string) error {
		calls++
		if calls < 2 {
			return assert.AnError
		}

		return nil
	}, 3*time.Second, 20*time.Millisecond)

	assert.True(t, ok)
	assert.GreaterOrEqual(t, calls, 2, "a failed handshake must not end the wait")
}
