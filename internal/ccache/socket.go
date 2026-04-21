package ccache

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

const (
	awaitReadyTimeout  = 5 * time.Second
	awaitReadyInterval = 100 * time.Millisecond
)

// Socket provides IPC communication and lifecycle management for the ccache
// storage helper at a given Unix socket path.
type Socket struct {
	path string
}

// NewSocket creates a Socket for the given path.
func NewSocket(path string) *Socket {
	return &Socket{path: path}
}

// Path returns the Unix socket path.
func (s *Socket) Path() string {
	return s.path
}

// IsListening returns true if the storage helper is actively listening.
func (s *Socket) IsListening() bool {
	return IsListening(s.path)
}

// Start launches the storage helper as a detached background process.
func (s *Socket) Start() error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	cmd := exec.Command(bin, "ccache", "storage-helper", "start") //nolint:gosec,noctx // intentionally detached: the helper must outlive this command
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start storage helper process: %w", err)
	}

	return nil
}

// AwaitReady polls until the socket is listening or a 5-second timeout elapses.
func (s *Socket) AwaitReady() bool {
	deadline := time.Now().Add(awaitReadyTimeout)

	for time.Now().Before(deadline) {
		if IsListening(s.path) {
			return true
		}

		time.Sleep(awaitReadyInterval)
	}

	return false
}

// HealthCheck sends a health-check request to verify the server is ready.
func (s *Socket) HealthCheck(ctx context.Context) error {
	return SendHealthCheck(ctx, s.path)
}

// SetInvocationID notifies the server of a new parent→child invocation pair.
func (s *Socket) SetInvocationID(ctx context.Context, parentID, childID string) error {
	return SendInvocationID(ctx, s.path, parentID, childID)
}

// Stop sends a stop request to the storage helper.
func (s *Socket) Stop(ctx context.Context) error {
	return SendStop(ctx, s.path)
}

// GetSessionStats returns the accumulated session stats from the running helper,
// including byte counts and the active invocation IDs.
func (s *Socket) GetSessionStats(ctx context.Context) (SessionStats, error) {
	return SendGetSessionStats(ctx, s.path)
}
