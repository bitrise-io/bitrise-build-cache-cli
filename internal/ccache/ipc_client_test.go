//go:build unit

package ccache

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/ccache/protocol"
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
		type receivedIDs struct{ parentID, childID string }
		receivedIDCh := make(chan receivedIDs, 1)

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

			parentID, childID, readErr := protocol.ReadSetInvocationID(conn)
			if readErr != nil {
				errCh <- readErr
				return
			}
			receivedIDCh <- receivedIDs{parentID, childID}

			if writeErr := protocol.WriteOK(conn); writeErr != nil {
				errCh <- writeErr
				return
			}
			errCh <- nil
		}()

		err = SendInvocationID(context.Background(), socketPath, "my-parent-id", "my-child-id")
		assert.NoError(t, err)

		serverErr := <-errCh
		assert.NoError(t, serverErr)

		got := <-receivedIDCh
		assert.Equal(t, "my-parent-id", got.parentID)
		assert.Equal(t, "my-child-id", got.childID)
	})

	t.Run("connection failure: nonexistent socket returns error", func(t *testing.T) {
		socketPath := shortTempSocket(t, "nx.sock")
		// Do not create a listener — the socket file does not exist.

		err := SendInvocationID(context.Background(), socketPath, "parent-id", "child-id")
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

			// Read the invocation ID messages
			if _, _, readErr := protocol.ReadSetInvocationID(conn); readErr != nil {
				errCh <- readErr
				return
			}

			if writeErr := protocol.WriteErr(conn, "boom"); writeErr != nil {
				errCh <- writeErr
				return
			}
			errCh <- nil
		}()

		err = SendInvocationID(context.Background(), socketPath, "parent-id", "child-id")
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

			// Read the invocation ID messages
			if _, _, readErr := protocol.ReadSetInvocationID(conn); readErr != nil {
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

		err = SendInvocationID(context.Background(), socketPath, "parent-id", "child-id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected response")

		serverErr := <-errCh
		assert.NoError(t, serverErr)
	})
}

func Test_SendHealthCheck(t *testing.T) {
	t.Run("success: server writes greeting, reads request, writes OK", func(t *testing.T) {
		socketPath := shortTempSocket(t, "hc.sock")
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

			reqType, readErr := protocol.ReadByte(conn)
			if readErr != nil {
				errCh <- readErr
				return
			}
			if reqType != protocol.RequestHealthCheck {
				errCh <- nil
				return
			}

			errCh <- protocol.WriteOK(conn)
		}()

		err = SendHealthCheck(context.Background(), socketPath)
		assert.NoError(t, err)
		assert.NoError(t, <-errCh)
	})

	t.Run("connection failure: nonexistent socket returns error", func(t *testing.T) {
		socketPath := shortTempSocket(t, "nx-hc.sock")

		err := SendHealthCheck(context.Background(), socketPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "connect to ccache socket")
	})

	t.Run("server error response returns error", func(t *testing.T) {
		socketPath := shortTempSocket(t, "err-hc.sock")
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

			if _, readErr := protocol.ReadByte(conn); readErr != nil {
				errCh <- readErr
				return
			}

			errCh <- protocol.WriteErr(conn, "boom")
		}()

		err = SendHealthCheck(context.Background(), socketPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server error: boom")
		assert.NoError(t, <-errCh)
	})

	t.Run("unexpected response byte returns error", func(t *testing.T) {
		socketPath := shortTempSocket(t, "ux-hc.sock")
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

			if _, readErr := protocol.ReadByte(conn); readErr != nil {
				errCh <- readErr
				return
			}

			errCh <- protocol.WriteByte(conn, 0xFF)
		}()

		err = SendHealthCheck(context.Background(), socketPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected response")
		assert.NoError(t, <-errCh)
	})
}
