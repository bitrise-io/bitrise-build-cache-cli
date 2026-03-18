package ccache

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/protocol"
)

const (
	defaultDialTimeout = 2 * time.Second
	isListeningTimeout = 100 * time.Millisecond
)

// IsListening returns true if a process is actively listening on the given Unix socket path.
// It reads the server greeting before closing so the server sees a clean EOF rather than a broken pipe.
func IsListening(socketPath string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), isListeningTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	if err != nil {
		return false
	}
	defer conn.Close()
	_ = protocol.ReadGreeting(conn)

	return true
}

// SendInvocationID connects to the ccache storage helper Unix socket and notifies
// it of a parent→child invocation pair. The helper uses childID for per-invocation
// logging and session tracking, and registers the parent→child relationship.
// Returns an error if the connection or protocol exchange fails.
func SendInvocationID(ctx context.Context, socketPath, parentID, childID string) error {
	conn, err := (&net.Dialer{Timeout: defaultDialTimeout}).DialContext(ctx, "unix", socketPath)
	if err != nil {
		return fmt.Errorf("connect to ccache socket %s: %w", socketPath, err)
	}
	defer conn.Close()

	if err := protocol.ReadGreeting(conn); err != nil {
		return fmt.Errorf("read greeting: %w", err)
	}

	if err := protocol.WriteSetInvocationID(conn, parentID, childID); err != nil {
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
