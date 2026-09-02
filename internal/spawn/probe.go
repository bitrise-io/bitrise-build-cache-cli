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

// Handshake proves a service is answering, not merely accepting. Supplied by
// the caller so this package needs no service's protocol.
type Handshake func(ctx context.Context, socketPath string) error

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

// ProbeWith is Probe with a caller-supplied handshake, for a service where
// accepting a connection is not proof of serving. The ccache helper needs one:
// a dial and close mid-protocol surfaces as "Capabilities check failed" in its
// log, and CI asserts on those lines. Taking the handshake as a parameter keeps
// this package free of any one service's protocol.
func ProbeWith(ctx context.Context, path string, handshake Handshake) SocketState {
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

// AwaitSocketWith is AwaitSocket for a service with a handshake.
func AwaitSocketWith(
	ctx context.Context,
	path string,
	handshake Handshake,
	budget, interval time.Duration,
) bool {
	serving := func() bool {
		if handshake != nil {
			return ProbeWith(ctx, path, handshake) == Running
		}

		return Probe(ctx, path) == Running
	}

	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return false
		}

		if serving() {
			return true
		}

		time.Sleep(interval)
	}

	return serving()
}
