package ccache

import (
	"context"
	"fmt"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/spawn"
)

const (
	awaitReadyTimeout  = 5 * time.Second
	awaitReadyInterval = 100 * time.Millisecond
)

// HelperArgs exposes the argv the options produce, for assertions.
func HelperArgs(opts ...StartOption) []string {
	cfg := startConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	return helperService(cfg).Args
}

func helperService(cfg startConfig) spawn.Service {
	svc := spawn.CcacheHelper()
	if cfg.debug {
		svc = svc.WithDebug()
	}
	if cfg.invocationID != "" {
		svc = svc.WithArgs("--invocation-id=" + cfg.invocationID)
	}

	return svc
}

// Test seam for the detached spawn.
//
//nolint:gochecknoglobals
var detach = spawn.Detached

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

type StartOption func(*startConfig)

type startConfig struct {
	invocationID string
	debug        bool
}

func WithInvocationID(id string) StartOption {
	return func(c *startConfig) { c.invocationID = id }
}

func WithDebug() StartOption {
	return func(c *startConfig) { c.debug = true }
}

// Start launches the storage helper as a detached background process.
func (s *Socket) Start(opts ...StartOption) error {
	cfg := startConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	if _, err := detach(helperService(cfg)); err != nil {
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
