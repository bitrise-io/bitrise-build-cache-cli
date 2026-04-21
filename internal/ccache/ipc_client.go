package ccache

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/ccache/protocol"
)

const (
	defaultDialTimeout = 2 * time.Second
	isListeningTimeout = 100 * time.Millisecond
)

// SessionStats holds the stats returned by a GetSessionStats IPC call.
type SessionStats struct {
	DownloadedBytes int64
	UploadedBytes   int64
	InvocationID    string
	ParentID        string
}

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

// SendStop connects to the ccache storage helper and sends a STOP request, causing
// the server to flush final stats and shut down. Blocks until the server ACKs, meaning
// the onShutdown callback has completed before this returns.
func SendStop(ctx context.Context, socketPath string) error {
	conn, err := (&net.Dialer{Timeout: defaultDialTimeout}).DialContext(ctx, "unix", socketPath)
	if err != nil {
		return fmt.Errorf("connect to ccache socket %s: %w", socketPath, err)
	}
	defer conn.Close()

	if err := protocol.ReadGreeting(conn); err != nil {
		return fmt.Errorf("read greeting: %w", err)
	}

	if err := protocol.WriteByte(conn, protocol.RequestStop); err != nil {
		return fmt.Errorf("send stop request: %w", err)
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

// SendGetSessionStats connects to the ccache storage helper and requests the current
// accumulated downloaded and uploaded byte counts for the active session, along with
// the active invocation ID and parent invocation ID.
func SendGetSessionStats(ctx context.Context, socketPath string) (SessionStats, error) {
	conn, err := (&net.Dialer{Timeout: defaultDialTimeout}).DialContext(ctx, "unix", socketPath)
	if err != nil {
		return SessionStats{}, fmt.Errorf("connect to ccache socket %s: %w", socketPath, err)
	}
	defer conn.Close()

	if err := protocol.ReadGreeting(conn); err != nil {
		return SessionStats{}, fmt.Errorf("read greeting: %w", err)
	}

	if err := protocol.WriteByte(conn, protocol.RequestGetSessionStats); err != nil {
		return SessionStats{}, fmt.Errorf("send get-session-stats request: %w", err)
	}

	resp, err := protocol.ReadByte(conn)
	if err != nil {
		return SessionStats{}, fmt.Errorf("read response: %w", err)
	}

	switch resp {
	case protocol.ResponseOK:
		dl, ul, invocationID, parentID, err := protocol.ReadSessionStats(conn)
		if err != nil {
			return SessionStats{}, fmt.Errorf("read session stats: %w", err)
		}

		return SessionStats{
			DownloadedBytes: dl,
			UploadedBytes:   ul,
			InvocationID:    invocationID,
			ParentID:        parentID,
		}, nil
	case protocol.ResponseErr:
		msg, _ := protocol.ReadMsg(conn)

		return SessionStats{}, fmt.Errorf("server error: %s", msg)
	default:
		return SessionStats{}, fmt.Errorf("unexpected response: 0x%02x", resp)
	}
}

// SendHealthCheck connects to the ccache storage helper and sends a health-check request.
// Returns nil if the server is up and responding, or an error if unreachable or unhealthy.
func SendHealthCheck(ctx context.Context, socketPath string) error {
	conn, err := (&net.Dialer{Timeout: defaultDialTimeout}).DialContext(ctx, "unix", socketPath)
	if err != nil {
		return fmt.Errorf("connect to ccache socket %s: %w", socketPath, err)
	}
	defer conn.Close()

	if err := protocol.ReadGreeting(conn); err != nil {
		return fmt.Errorf("read greeting: %w", err)
	}

	if err := protocol.WriteByte(conn, protocol.RequestHealthCheck); err != nil {
		return fmt.Errorf("send health-check request: %w", err)
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
