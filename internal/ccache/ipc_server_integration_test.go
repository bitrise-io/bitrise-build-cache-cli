//go:build integration

package ccache

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/protocol"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
)

// integrationTempSocket creates a Unix socket path short enough for macOS (< 104 chars).
func integrationTempSocket(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "cc-integ-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	return filepath.Join(dir, name)
}

// waitForSocket polls IsListening until the server is ready or times out.
func waitForSocket(t *testing.T, socketPath string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if IsListening(socketPath) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for socket %s to become ready", socketPath)
}

// noOpLogger returns a logger that discards all output.
func noOpLogger() log.Logger {
	return log.NewLogger(log.WithOutput(io.Discard))
}

// noOpClient returns a ClientMock with stub functions that return zero values.
// Unset methods are nil-safe due to moq's -stub generation.
func noOpClient() *ClientMock {
	return &ClientMock{
		ChangeSessionFunc:            func(_, _, _, _ string) {},
		GetCapabilitiesWithRetryFunc: func(_ context.Context) error { return nil },
	}
}

// integrationConfig returns a minimal ccache config for integration tests.
func integrationConfig(socketPath string) ccacheconfig.Config {
	return ccacheconfig.Config{
		IPCEndpoint: socketPath,
		PushEnabled: true,
	}
}

// startTestServer creates and starts an IpcServer in a goroutine.
// It returns the server, a done channel (closed when Run returns), and the cancel func.
// The caller must call cancel() to shut down the server if not using SendStop.
func startTestServer(
	t *testing.T,
	socketPath string,
	client Client,
	loggerFactory LoggerFactory,
) (*IpcServer, context.CancelFunc, <-chan error) {
	t.Helper()
	cfg := integrationConfig(socketPath)
	server, err := NewServer(
		cfg,
		configcommon.CacheConfigMetadata{},
		client,
		noOpLogger(),
		loggerFactory,
		"initial-id",
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- server.Run(ctx)
	}()

	waitForSocket(t, socketPath, 2*time.Second)

	return server, cancel, done
}

// buildIntegrationSetInvocationIDRequest builds a SetInvocationID protocol message.
func buildIntegrationSetInvocationIDRequest(parentID, childID string) []byte {
	var buf bytes.Buffer
	buf.WriteByte(protocol.RequestSetInvocationID)
	buf.WriteByte(byte(len(parentID)))
	buf.WriteString(parentID)
	buf.WriteByte(byte(len(childID)))
	buf.WriteString(childID)

	return buf.Bytes()
}

// buildIntegrationGetRequest builds a GET protocol message.
func buildIntegrationGetRequest(key []byte) []byte {
	var buf bytes.Buffer
	buf.WriteByte(protocol.RequestGet)
	buf.WriteByte(byte(len(key)))
	buf.Write(key)

	return buf.Bytes()
}

// buildIntegrationPutRequest builds a PUT protocol message.
func buildIntegrationPutRequest(key, value []byte) []byte {
	var buf bytes.Buffer
	buf.WriteByte(protocol.RequestPut)
	buf.WriteByte(byte(len(key)))
	buf.Write(key)
	buf.WriteByte(0x00) // flags
	sizeBytes := make([]byte, 8)
	binary.NativeEndian.PutUint64(sizeBytes, uint64(len(value)))
	buf.Write(sizeBytes)
	buf.Write(value)

	return buf.Bytes()
}

// sendRequest dials the socket, reads the greeting, sends request bytes, reads and returns the response byte.
func sendRequest(t *testing.T, socketPath string, reqBytes []byte) byte {
	t.Helper()
	conn, err := net.Dial("unix", socketPath)
	require.NoError(t, err)
	defer conn.Close()

	require.NoError(t, protocol.ReadGreeting(conn))
	_, err = conn.Write(reqBytes)
	require.NoError(t, err)

	resp, err := protocol.ReadByte(conn)
	require.NoError(t, err)

	return resp
}

// sendRequestAndReadValue dials the socket, sends a GET request, and reads the full response including value.
func sendGetAndReadValue(t *testing.T, socketPath string, key []byte) (byte, []byte) {
	t.Helper()
	conn, err := net.Dial("unix", socketPath)
	require.NoError(t, err)
	defer conn.Close()

	require.NoError(t, protocol.ReadGreeting(conn))
	_, err = conn.Write(buildIntegrationGetRequest(key))
	require.NoError(t, err)

	resp, err := protocol.ReadByte(conn)
	require.NoError(t, err)

	if resp != protocol.ResponseOK {
		return resp, nil
	}

	data, err := protocol.ReadValue(conn)
	require.NoError(t, err)

	return resp, data
}

// buildIntegrationStopRequest builds a STOP protocol message.
func buildIntegrationStopRequest() []byte {
	return []byte{protocol.RequestStop}
}

// sendRequestsOnConn dials the socket once, reads the greeting, then sends each request
// in sequence on the same connection and returns the response bytes in order.
// Using a single connection guarantees that activeInvocationID is updated between
// consecutive requests — the server goroutine updates it before looping back to read the
// next request, so there is no race with a concurrent STOP arriving on a different goroutine.
func sendRequestsOnConn(t *testing.T, socketPath string, requests ...[]byte) []byte {
	t.Helper()
	conn, err := net.Dial("unix", socketPath)
	require.NoError(t, err)
	defer conn.Close()

	require.NoError(t, protocol.ReadGreeting(conn))

	responses := make([]byte, 0, len(requests))
	for _, req := range requests {
		_, writeErr := conn.Write(req)
		require.NoError(t, writeErr)

		resp, readErr := protocol.ReadByte(conn)
		require.NoError(t, readErr)
		responses = append(responses, resp)
	}

	return responses
}

func Test_IpcServer_Integration_GetSessionStats_returns_accumulated_bytes(t *testing.T) {
	socketPath := integrationTempSocket(t, "s.sock")

	const downloadData = "hello ccache"
	client := noOpClient()
	client.DownloadStreamFunc = func(_ context.Context, w io.Writer, _ string) error {
		_, err := w.Write([]byte(downloadData))

		return err
	}

	_, cancel, serverDone := startTestServer(t, socketPath, client, nil)
	defer cancel()

	// Accumulate download bytes via a GET
	resp, data := sendGetAndReadValue(t, socketPath, []byte{0x01})
	require.Equal(t, byte(protocol.ResponseOK), resp)
	require.Equal(t, []byte(downloadData), data)

	// GetSessionStats should return the accumulated bytes without resetting them
	dl, ul, err := SendGetSessionStats(context.Background(), socketPath)
	require.NoError(t, err)
	assert.Equal(t, int64(len(downloadData)), dl)
	assert.Equal(t, int64(0), ul)

	// A second call should return the same bytes (not reset by the query)
	dl2, ul2, err := SendGetSessionStats(context.Background(), socketPath)
	require.NoError(t, err)
	assert.Equal(t, dl, dl2)
	assert.Equal(t, ul, ul2)

	cancel()
	<-serverDone
}

func Test_IpcServer_Integration_GetSessionStats_zero_when_no_activity(t *testing.T) {
	socketPath := integrationTempSocket(t, "s.sock")

	_, cancel, serverDone := startTestServer(t, socketPath, noOpClient(), nil)
	defer cancel()

	dl, ul, err := SendGetSessionStats(context.Background(), socketPath)
	require.NoError(t, err)
	assert.Equal(t, int64(0), dl)
	assert.Equal(t, int64(0), ul)

	cancel()
	<-serverDone
}

func Test_IpcServer_Integration_HealthCheck_returns_OK(t *testing.T) {
	socketPath := integrationTempSocket(t, "hc.sock")

	_, cancel, serverDone := startTestServer(t, socketPath, noOpClient(), nil)
	defer cancel()

	err := SendHealthCheck(context.Background(), socketPath)
	require.NoError(t, err)

	cancel()
	<-serverDone
}

func Test_IpcServer_Integration_Stop_shuts_down_server(t *testing.T) {
	socketPath := integrationTempSocket(t, "stop.sock")

	_, cancel, serverDone := startTestServer(t, socketPath, noOpClient(), nil)
	defer cancel()

	// Mirrors what stopStorageHelperCmd does: get stats then stop.
	dl, ul, err := SendGetSessionStats(context.Background(), socketPath)
	require.NoError(t, err)
	assert.Equal(t, int64(0), dl)
	assert.Equal(t, int64(0), ul)

	require.NoError(t, SendStop(context.Background(), socketPath))

	// Server should have shut down.
	select {
	case serverErr := <-serverDone:
		assert.NoError(t, serverErr)
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down after SendStop")
	}

	assert.False(t, IsListening(socketPath), "socket should no longer be listening after stop")
}
