package ccache

import (
	"fmt"
	"net"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/protocol"
)

const defaultDialTimeout = 2 * time.Second
const isListeningTimeout = 100 * time.Millisecond

// IsListening returns true if a process is actively listening on the given Unix socket path.
func IsListening(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, isListeningTimeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// SendInvocationID connects to the ccache storage helper Unix socket and notifies
// it of the current invocation ID. The helper uses it for per-invocation logging.
// Returns an error if the connection or protocol exchange fails.
func SendInvocationID(socketPath, invocationID string) error {
	conn, err := net.DialTimeout("unix", socketPath, defaultDialTimeout)
	if err != nil {
		return fmt.Errorf("connect to ccache socket %s: %w", socketPath, err)
	}
	defer conn.Close()

	if err := protocol.ReadGreeting(conn); err != nil {
		return fmt.Errorf("read greeting: %w", err)
	}

	if err := protocol.WriteSetInvocationID(conn, invocationID); err != nil {
		return fmt.Errorf("send invocation ID: %w", err)
	}

	resp, err := protocol.ReadByte(conn)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	switch resp {
	case protocol.ResponseOK:
		return nil
	case protocol.ResponseErr:
		msg, _ := protocol.ReadMsg(conn)
		return fmt.Errorf("server error: %s", msg)
	default:
		return fmt.Errorf("unexpected response: 0x%02x", resp)
	}
}
