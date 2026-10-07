//go:build unit && darwin

package proxy_test

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/proxy"
)

func TestPeerListener_TagsPIDOnDarwin(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "s.sock")

	raw, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })

	pl := proxy.NewPeerListener(raw)

	// Dial from this process so we know the expected PID.
	done := make(chan struct{})
	var accepted net.Conn

	go func() {
		defer close(done)

		c, acceptErr := pl.Accept()
		if acceptErr != nil {
			return
		}

		accepted = c
	}()

	client, err := net.Dial("unix", sockPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Accept did not return within 2s")
	}

	require.NotNil(t, accepted)

	pc, ok := accepted.(interface {
		PeerPIDForTest() int
		RemoteAddrTagForTest() string
	})
	require.True(t, ok, "accepted conn should expose test accessors")

	assert.Equal(t, os.Getpid(), pc.PeerPIDForTest(), "peer PID should match this process")
	assert.NotEmpty(t, pc.RemoteAddrTagForTest(), "remote addr tag should be non-empty")

	looked, found := pl.LookupConn(pc.RemoteAddrTagForTest())
	assert.True(t, found)
	assert.NotNil(t, looked)

	pl.Forget(pc.RemoteAddrTagForTest())

	_, found = pl.LookupConn(pc.RemoteAddrTagForTest())
	assert.False(t, found, "Forget should remove the entry")
}
