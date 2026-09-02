package spawn

import (
	"context"
	"net"
	"os"
	"time"
)

type SocketState int

const (
	Running SocketState = iota
	Stopped
	Stuck
)

const ProbeTimeout = 500 * time.Millisecond

// Probe dials a unix socket. A hung accept still counts as running.
func Probe(ctx context.Context, path string) SocketState {
	if _, err := os.Stat(path); err != nil {
		return Stopped
	}

	dialCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(dialCtx, "unix", path)
	if err != nil {
		return Stuck
	}
	_ = conn.Close()

	return Running
}

// AwaitSocket polls until the socket serves or the budget runs out. Returning
// before it answers lets the build race the service and fail its first cache
// operations.
func AwaitSocket(ctx context.Context, path string, budget, interval time.Duration) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return false
		}

		if Probe(ctx, path) == Running {
			return true
		}

		time.Sleep(interval)
	}

	return Probe(ctx, path) == Running
}
