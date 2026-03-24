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
	"sync"
	"sync/atomic"
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
	onChildInvocation func(prevInvocationID, parentID, childID string, dl, ul int64),
	onShutdown func(invocationID string, dl, ul int64),
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
		onChildInvocation,
		onShutdown,
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

func Test_IpcServer_Integration_SetInvocationID_fires_onChildInvocation(t *testing.T) {
	socketPath := integrationTempSocket(t, "s.sock")

	type invocationCall struct {
		prevID   string
		parentID string
		childID  string
		dl       int64
		ul       int64
	}

	var mu sync.Mutex
	var calls []invocationCall
	done := make(chan struct{})

	onChild := func(prevID, parentID, childID string, dl, ul int64) {
		mu.Lock()
		calls = append(calls, invocationCall{prevID, parentID, childID, dl, ul})
		mu.Unlock()
		close(done)
	}

	_, cancel, serverDone := startTestServer(t, socketPath, noOpClient(), nil, onChild, nil)
	defer cancel()

	resp := sendRequest(t, socketPath, buildIntegrationSetInvocationIDRequest("parent-1", "child-1"))
	assert.Equal(t, byte(protocol.ResponseOK), resp)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("onChildInvocation was not called within timeout")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, calls, 1)
	// prevID is the initial invocation ID active before the first SetInvocationID
	assert.Equal(t, "initial-id", calls[0].prevID)
	assert.Equal(t, "parent-1", calls[0].parentID)
	assert.Equal(t, "child-1", calls[0].childID)
	assert.Equal(t, int64(0), calls[0].dl)
	assert.Equal(t, int64(0), calls[0].ul)

	cancel()
	<-serverDone
}

func Test_IpcServer_Integration_SetInvocationID_reports_accumulated_bytes(t *testing.T) {
	socketPath := integrationTempSocket(t, "s.sock")

	const downloadData = "hello ccache"
	client := noOpClient()
	client.DownloadStreamFunc = func(_ context.Context, w io.Writer, _ string) error {
		_, err := w.Write([]byte(downloadData))

		return err
	}

	type invocationCall struct {
		dl int64
		ul int64
	}

	var mu sync.Mutex
	var calls []invocationCall
	childCalled := make(chan struct{})

	onChild := func(_, _, _ string, dl, ul int64) {
		mu.Lock()
		calls = append(calls, invocationCall{dl, ul})
		mu.Unlock()
		close(childCalled)
	}

	_, cancel, serverDone := startTestServer(t, socketPath, client, nil, onChild, nil)
	defer cancel()

	// Perform a GET to accumulate download bytes
	resp, data := sendGetAndReadValue(t, socketPath, []byte{0x01})
	assert.Equal(t, byte(protocol.ResponseOK), resp)
	assert.Equal(t, []byte(downloadData), data)

	// Now send SetInvocationID — should trigger callback with accumulated bytes
	resp = sendRequest(t, socketPath, buildIntegrationSetInvocationIDRequest("parent-1", "child-1"))
	assert.Equal(t, byte(protocol.ResponseOK), resp)

	select {
	case <-childCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("onChildInvocation was not called within timeout")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, calls, 1)
	assert.Equal(t, int64(len(downloadData)), calls[0].dl)
	assert.Equal(t, int64(0), calls[0].ul)

	cancel()
	<-serverDone
}

func Test_IpcServer_Integration_SendStop_fires_onShutdown_synchronously(t *testing.T) {
	socketPath := integrationTempSocket(t, "s.sock")

	var shutdownCalled atomic.Bool
	var capturedID string
	shutdownDone := make(chan struct{})

	onShutdown := func(invocationID string, _, _ int64) {
		capturedID = invocationID
		shutdownCalled.Store(true)
		close(shutdownDone)
	}

	_, cancel, serverDone := startTestServer(t, socketPath, noOpClient(), nil, nil, onShutdown)
	defer cancel()

	err := SendStop(context.Background(), socketPath)
	require.NoError(t, err)

	// SendStop blocks until server ACKs, and ACK is sent after onShutdown completes.
	// So by the time SendStop returns, shutdownCalled must already be true.
	assert.True(t, shutdownCalled.Load(), "onShutdown should have been called before SendStop returns")

	select {
	case <-shutdownDone:
	case <-time.After(2 * time.Second):
		t.Fatal("onShutdown was not called")
	}

	// No SetInvocationID was sent, so the initial ID should be reported.
	assert.Equal(t, "initial-id", capturedID)

	// Server should shut down cleanly
	select {
	case err := <-serverDone:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down after STOP")
	}
}

func Test_IpcServer_Integration_IdleTimeout_fires_onShutdown(t *testing.T) {
	socketPath := integrationTempSocket(t, "s.sock")

	var capturedID string
	shutdownCalled := make(chan struct{})

	onShutdown := func(invocationID string, _, _ int64) {
		capturedID = invocationID
		close(shutdownCalled)
	}

	cfg := ccacheconfig.Config{
		IPCEndpoint: socketPath,
		PushEnabled: true,
		IdleTimeout: 100 * time.Millisecond,
	}

	server, err := NewServer(
		cfg,
		configcommon.CacheConfigMetadata{},
		noOpClient(),
		noOpLogger(),
		nil,
		"initial-id",
		nil,
		onShutdown,
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverDone := make(chan error, 1)

	go func() {
		serverDone <- server.Run(ctx)
	}()

	waitForSocket(t, socketPath, 2*time.Second)

	// Don't send any requests — let idle timeout fire
	select {
	case <-shutdownCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("onShutdown was not called by idle timeout")
	}

	// No SetInvocationID was sent, so the initial ID should be reported.
	assert.Equal(t, "initial-id", capturedID)

	select {
	case err := <-serverDone:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down after idle timeout")
	}
}

func Test_IpcServer_Integration_STOP_then_idle_timeout_onShutdown_called_once(t *testing.T) {
	socketPath := integrationTempSocket(t, "s.sock")

	var shutdownCount atomic.Int32

	onShutdown := func(_ string, _, _ int64) {
		shutdownCount.Add(1)
	}

	cfg := ccacheconfig.Config{
		IPCEndpoint: socketPath,
		PushEnabled: true,
		IdleTimeout: 100 * time.Millisecond,
	}

	server, err := NewServer(
		cfg,
		configcommon.CacheConfigMetadata{},
		noOpClient(),
		noOpLogger(),
		nil,
		"initial-id",
		nil,
		onShutdown,
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverDone := make(chan error, 1)

	go func() {
		serverDone <- server.Run(ctx)
	}()

	waitForSocket(t, socketPath, 2*time.Second)

	// Send STOP — fires onShutdown
	err = SendStop(context.Background(), socketPath)
	require.NoError(t, err)

	// Wait for server to shut down, then wait past the idle timeout window
	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down after STOP")
	}

	time.Sleep(200 * time.Millisecond) // let idle timeout window pass

	assert.Equal(t, int32(1), shutdownCount.Load(), "onShutdown should be called exactly once")
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

func Test_IpcServer_Integration_sequential_SetInvocationID_prevID_chain(t *testing.T) {
	socketPath := integrationTempSocket(t, "s.sock")

	type call struct {
		prevID  string
		childID string
	}

	calls := make(chan call, 2)

	onChild := func(prevID, _, childID string, _, _ int64) {
		calls <- call{prevID, childID}
	}

	_, cancel, serverDone := startTestServer(t, socketPath, noOpClient(), nil, onChild, nil)
	defer cancel()

	// First SetInvocationID
	resp := sendRequest(t, socketPath, buildIntegrationSetInvocationIDRequest("parent", "child-1"))
	assert.Equal(t, byte(protocol.ResponseOK), resp)

	var first call
	select {
	case first = <-calls:
	case <-time.After(2 * time.Second):
		t.Fatal("first onChildInvocation not called")
	}

	// Second SetInvocationID — waiting for the first callback ensures activeInvocationID
	// has been updated to "child-1" before we send the second request.
	resp = sendRequest(t, socketPath, buildIntegrationSetInvocationIDRequest("parent", "child-2"))
	assert.Equal(t, byte(protocol.ResponseOK), resp)

	var second call
	select {
	case second = <-calls:
	case <-time.After(2 * time.Second):
		t.Fatal("second onChildInvocation not called")
	}

	assert.Equal(t, "initial-id", first.prevID)
	assert.Equal(t, "child-1", first.childID)
	assert.Equal(t, "child-1", second.prevID)
	assert.Equal(t, "child-2", second.childID)

	cancel()
	<-serverDone
}

func Test_IpcServer_Integration_onShutdown_receives_last_active_id(t *testing.T) {
	socketPath := integrationTempSocket(t, "s.sock")

	childCalled := make(chan struct{})
	onChild := func(_, _, _ string, _, _ int64) {
		close(childCalled)
	}

	var capturedID string
	shutdownDone := make(chan struct{})
	onShutdown := func(invocationID string, _, _ int64) {
		capturedID = invocationID
		close(shutdownDone)
	}

	_, cancel, serverDone := startTestServer(t, socketPath, noOpClient(), nil, onChild, onShutdown)
	defer cancel()

	resp := sendRequest(t, socketPath, buildIntegrationSetInvocationIDRequest("parent", "child-1"))
	assert.Equal(t, byte(protocol.ResponseOK), resp)

	// Wait for the callback — this guarantees activeInvocationID is now "child-1"
	// before we send STOP on a new connection.
	select {
	case <-childCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("onChildInvocation not called")
	}

	err := SendStop(context.Background(), socketPath)
	require.NoError(t, err)

	select {
	case <-shutdownDone:
	case <-time.After(2 * time.Second):
		t.Fatal("onShutdown not called")
	}

	assert.Equal(t, "child-1", capturedID)

	<-serverDone
}

func Test_IpcServer_Integration_activeID_updated_when_onChildInvocation_nil(t *testing.T) {
	socketPath := integrationTempSocket(t, "s.sock")

	var capturedID string
	shutdownDone := make(chan struct{})
	onShutdown := func(invocationID string, _, _ int64) {
		capturedID = invocationID
		close(shutdownDone)
	}

	// onChildInvocation is nil — activeInvocationID must still be updated on SetInvocationID
	_, cancel, serverDone := startTestServer(t, socketPath, noOpClient(), nil, nil, onShutdown)
	defer cancel()

	// Send both requests on the same connection so they are processed sequentially
	// by the same goroutine. This guarantees the activeInvocationID update from
	// SetInvocationID happens before the STOP handler reads it, without relying on
	// a callback or a sleep for synchronization.
	resps := sendRequestsOnConn(t, socketPath,
		buildIntegrationSetInvocationIDRequest("parent", "child-1"),
		buildIntegrationStopRequest(),
	)
	assert.Equal(t, byte(protocol.ResponseOK), resps[0])
	assert.Equal(t, byte(protocol.ResponseOK), resps[1])

	select {
	case <-shutdownDone:
	case <-time.After(2 * time.Second):
		t.Fatal("onShutdown not called")
	}

	assert.Equal(t, "child-1", capturedID)

	<-serverDone
}
