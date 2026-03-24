//go:build integration

package ccache

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/protocol"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
)

func cbIntegTempSocket(t *testing.T) string {
	t.Helper()
	// Use a short prefix so the socket path stays under macOS's 104-char limit.
	//nolint:usetesting
	dir, err := os.MkdirTemp("", "cb-integ-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	return filepath.Join(dir, "s.sock")
}

func cbIntegNoOpLogger() log.Logger {
	return log.NewLogger(log.WithOutput(io.Discard))
}

// cbNoOpClient implements ccache.Client with no-op stubs.
type cbNoOpClient struct{}

func (cbNoOpClient) ChangeSession(_, _, _, _ string) {}

func (cbNoOpClient) DownloadStream(_ context.Context, _ io.Writer, _ string) error { return nil }

func (cbNoOpClient) UploadStreamToBuildCache(_ context.Context, _ io.ReadSeeker, _ string, _ int64) error {
	return nil
}

func (cbNoOpClient) GetCapabilitiesWithRetry(_ context.Context) error { return nil }

func cbIntegSendSetInvocationID(t *testing.T, socketPath, parentID, childID string) {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteByte(protocol.RequestSetInvocationID)
	buf.WriteByte(byte(len(parentID)))
	buf.WriteString(parentID)
	buf.WriteByte(byte(len(childID)))
	buf.WriteString(childID)

	d := net.Dialer{}
	conn, err := d.DialContext(t.Context(), "unix", socketPath)
	require.NoError(t, err)
	defer conn.Close()

	require.NoError(t, protocol.ReadGreeting(conn))
	_, err = conn.Write(buf.Bytes())
	require.NoError(t, err)

	resp, err := protocol.ReadByte(conn)
	require.NoError(t, err)
	assert.Equal(t, byte(protocol.ResponseOK), resp)
}

func cbIntegWaitForSocket(t *testing.T, socketPath string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ccache.IsListening(socketPath) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for socket %s", socketPath)
}

// Test_StorageHelperCallbacks_Integration_parent_chain verifies that:
//   - The first onChildInvocation receives parentInvocationID as prevParentID
//   - The second onChildInvocation receives the first call's parentID as prevParentID
//   - onShutdown receives the last call's parentID as activeParentID
func Test_StorageHelperCallbacks_Integration_parent_chain(t *testing.T) {
	socketPath := cbIntegTempSocket(t)

	type collectArgs struct {
		invocationID string
		parentID     string
	}

	var mu sync.Mutex
	var collectCalls []collectArgs
	collected := make(chan struct{}, 3)

	collectFn := func(invocationID, parentID string, _, _ int64) {
		mu.Lock()
		collectCalls = append(collectCalls, collectArgs{invocationID, parentID})
		mu.Unlock()
		collected <- struct{}{}
	}

	onChild, onShutdown := buildStorageHelperCallbacks(
		"env-parent",
		func(_, _ string) {},
		collectFn,
		func() {},
	)

	cfg := ccacheconfig.Config{IPCEndpoint: socketPath, PushEnabled: true}
	server, err := ccache.NewServer(
		cfg,
		configcommon.CacheConfigMetadata{},
		cbNoOpClient{},
		cbIntegNoOpLogger(),
		nil,
		"initial-id",
		onChild,
		onShutdown,
	)
	require.NoError(t, err)

	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Run(t.Context()) }()

	cbIntegWaitForSocket(t, socketPath)

	// First SetInvocationID: prevID=initial-id, its parent should be env-parent
	cbIntegSendSetInvocationID(t, socketPath, "rn-run-1", "ccache-1")
	select {
	case <-collected:
	case <-time.After(2 * time.Second):
		t.Fatal("first collectFn not called")
	}

	// Second SetInvocationID: prevID=ccache-1, its parent should be rn-run-1
	cbIntegSendSetInvocationID(t, socketPath, "rn-run-2", "ccache-2")
	select {
	case <-collected:
	case <-time.After(2 * time.Second):
		t.Fatal("second collectFn not called")
	}

	// STOP: activeID=ccache-2, its parent should be rn-run-2
	err = ccache.SendStop(t.Context(), socketPath)
	require.NoError(t, err)
	select {
	case <-collected:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown collectFn not called")
	}

	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, collectCalls, 3)

	assert.Equal(t, "initial-id", collectCalls[0].invocationID)
	assert.Equal(t, "env-parent", collectCalls[0].parentID, "first child: prevID parent should be env-parent")

	assert.Equal(t, "ccache-1", collectCalls[1].invocationID)
	assert.Equal(t, "rn-run-1", collectCalls[1].parentID, "second child: prevID parent should be rn-run-1")

	assert.Equal(t, "ccache-2", collectCalls[2].invocationID)
	assert.Equal(t, "rn-run-2", collectCalls[2].parentID, "shutdown: activeID parent should be rn-run-2")
}
