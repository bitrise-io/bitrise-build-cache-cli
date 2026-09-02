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

// ProbeWith replaces the raw dial with a caller-supplied handshake. The ccache
// helper needs one: a dial+close surfaces as "Capabilities check failed" in its
// log, and CI asserts on those lines.
func ProbeWith(ctx context.Context, path string, handshake func(context.Context, string) error) SocketState {
	if _, err := os.Stat(path); err != nil {
		return Stopped
	}

	checkCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()

	if err := handshake(checkCtx, path); err != nil {
		return Stuck
	}

	return Running
}

// AwaitSocket polls until the socket serves or the budget runs out. Returning
// before it answers lets the build race the service and fail its first cache
// operations.
func AwaitSocket(ctx context.Context, path string, budget, interval time.Duration) bool {
	return AwaitSocketWith(ctx, path, nil, budget, interval)
}

// AwaitSocketWith is AwaitSocket with a caller-supplied handshake.
func AwaitSocketWith(
	ctx context.Context,
	path string,
	handshake func(context.Context, string) error,
	budget, interval time.Duration,
) bool {
	probe := func() SocketState {
		if handshake != nil {
			return ProbeWith(ctx, path, handshake)
		}

		return Probe(ctx, path)
	}

	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return false
		}

		if probe() == Running {
			return true
		}

		time.Sleep(interval)
	}

	return probe() == Running
}
