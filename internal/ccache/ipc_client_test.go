//go:build unit

package ccache

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shortTempSocket creates a Unix socket path short enough for macOS (< 104 chars).
func shortTempSocket(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "cc-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

func Test_SendInvocationID(t *testing.T) {
	t.Run("success: server writes greeting, reads request, writes OK", func(t *testing.T) {
		socketPath := shortTempSocket(t, "t.sock")
		listener, err := net.Listen("unix", socketPath)
		require.NoError(t, err)
		defer listener.Close()

		errCh := make(chan error, 1)
		receivedIDCh := make(chan string, 1)

		go func() {
			conn, accept := listener.Accept()
			if accept != nil {
				errCh <- accept
				return
			}
			defer conn.Close()

			if writeErr := protocol.WriteGreeting(conn); writeErr != nil {
				errCh <- writeErr
				return
			}

			// Read request type byte
			reqType, readErr := protocol.ReadByte(conn)
			if readErr != nil {
				errCh <- readErr
				return
			}
			if reqType != protocol.RequestSetInvocationID {
				errCh <- nil
				return
			}

			id, readErr := protocol.ReadSetInvocationID(conn)
			if readErr != nil {
				errCh <- readErr
				return
			}
			receivedIDCh <- id

			if writeErr := protocol.WriteOK(conn); writeErr != nil {
				errCh <- writeErr
				return
			}
			errCh <- nil
		}()

		err = SendInvocationID(socketPath, "my-inv-id")
		assert.NoError(t, err)

		serverErr := <-errCh
		assert.NoError(t, serverErr)

		receivedID := <-receivedIDCh
		assert.Equal(t, "my-inv-id", receivedID)
	})

	t.Run("connection failure: nonexistent socket returns error", func(t *testing.T) {
		socketPath := shortTempSocket(t, "nx.sock")
		// Do not create a listener — the socket file does not exist.

		err := SendInvocationID(socketPath, "inv-id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "connect to ccache socket")
	})

	t.Run("server error response: server writes greeting then WriteErr", func(t *testing.T) {
		socketPath := shortTempSocket(t, "e.sock")
		listener, err := net.Listen("unix", socketPath)
		require.NoError(t, err)
		defer listener.Close()

		errCh := make(chan error, 1)

		go func() {
			conn, accept := listener.Accept()
			if accept != nil {
				errCh <- accept
				return
			}
			defer conn.Close()

			if writeErr := protocol.WriteGreeting(conn); writeErr != nil {
				errCh <- writeErr
				return
			}

			// Read request type byte
			if _, readErr := protocol.ReadByte(conn); readErr != nil {
				errCh <- readErr
				return
			}

			// Read the invocation ID message
			if _, readErr := protocol.ReadSetInvocationID(conn); readErr != nil {
				errCh <- readErr
				return
			}

			if writeErr := protocol.WriteErr(conn, "boom"); writeErr != nil {
				errCh <- writeErr
				return
			}
			errCh <- nil
		}()

		err = SendInvocationID(socketPath, "inv-id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server error: boom")

		serverErr := <-errCh
		assert.NoError(t, serverErr)
	})

	t.Run("unexpected response byte returns error", func(t *testing.T) {
		socketPath := shortTempSocket(t, "u.sock")
		listener, err := net.Listen("unix", socketPath)
		require.NoError(t, err)
		defer listener.Close()

		errCh := make(chan error, 1)

		go func() {
			conn, accept := listener.Accept()
			if accept != nil {
				errCh <- accept
				return
			}
			defer conn.Close()

			if writeErr := protocol.WriteGreeting(conn); writeErr != nil {
				errCh <- writeErr
				return
			}

			// Read request type byte
			if _, readErr := protocol.ReadByte(conn); readErr != nil {
				errCh <- readErr
				return
			}

			// Read the invocation ID message
			if _, readErr := protocol.ReadSetInvocationID(conn); readErr != nil {
				errCh <- readErr
				return
			}

			// Write an unexpected response byte
			if writeErr := protocol.WriteByte(conn, 0xFF); writeErr != nil {
				errCh <- writeErr
				return
			}
			errCh <- nil
		}()

		err = SendInvocationID(socketPath, "inv-id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected response")

		serverErr := <-errCh
		assert.NoError(t, serverErr)
	})
}
