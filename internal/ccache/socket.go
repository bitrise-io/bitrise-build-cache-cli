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

// StartOption customizes how the storage-helper subprocess is spawned.
type StartOption func(*startConfig)

type startConfig struct {
	invocationID string
	debug        bool
}

// WithInvocationID passes --invocation-id=X to the helper. Callers that already
// know the build's invocation ID at spawn time (e.g. the wrapper's parent ID)
// get a deterministic ~/.local/state/ccache/logs/ccache-<id>.log filename
// instead of the helper's random-UUID default.
func WithInvocationID(id string) StartOption {
	return func(c *startConfig) { c.invocationID = id }
}

// WithDebug forwards --debug to the helper root command so [SetInvocationID]
// and per-op stat lines land in the log file. Consumers that assert on those
// lines in CI must enable this.
func WithDebug() StartOption {
	return func(c *startConfig) { c.debug = true }
}

// Start launches the storage helper as a detached background process.
func (s *Socket) Start(opts ...StartOption) error {
	cfg := startConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	args := make([]string, 0, 5)
	if cfg.debug {
		args = append(args, "--debug")
	}
	args = append(args, "ccache", "storage-helper", "start")
	if cfg.invocationID != "" {
		args = append(args, "--invocation-id="+cfg.invocationID)
	}

	cmd := exec.Command(bin, args...) //nolint:gosec,noctx // intentionally detached: the helper must outlive this command
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
