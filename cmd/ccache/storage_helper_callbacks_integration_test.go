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

// Test_StorageHelperCallbacks_Integration_register verifies that registerFn is called
// with the correct parentID and childID for each SetInvocationID message.
func Test_StorageHelperCallbacks_Integration_register(t *testing.T) {
	socketPath := cbIntegTempSocket(t)

	type registerArgs struct {
		parentID string
		childID  string
	}

	var mu sync.Mutex
	var registerCalls []registerArgs
	registered := make(chan struct{}, 2)

	onChild := buildStorageHelperCallbacks(
		func(parentID, childID string) {
			mu.Lock()
			registerCalls = append(registerCalls, registerArgs{parentID, childID})
			mu.Unlock()
			registered <- struct{}{}
		},
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
		nil,
	)
	require.NoError(t, err)

	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Run(t.Context()) }()

	cbIntegWaitForSocket(t, socketPath)

	cbIntegSendSetInvocationID(t, socketPath, "rn-run-1", "ccache-1")
	select {
	case <-registered:
	case <-time.After(2 * time.Second):
		t.Fatal("first registerFn not called")
	}

	cbIntegSendSetInvocationID(t, socketPath, "rn-run-2", "ccache-2")
	select {
	case <-registered:
	case <-time.After(2 * time.Second):
		t.Fatal("second registerFn not called")
	}

	err = ccache.SendStop(t.Context(), socketPath)
	require.NoError(t, err)

	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, registerCalls, 2)

	assert.Equal(t, registerArgs{"rn-run-1", "ccache-1"}, registerCalls[0])
	assert.Equal(t, registerArgs{"rn-run-2", "ccache-2"}, registerCalls[1])
}
