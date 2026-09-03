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
//
// The ccache helper needs one: a dial and close mid-protocol surfaces as
// "Capabilities check failed" in its log, and CI asserts on those lines.
type Handshake func(ctx context.Context, socketPath string) error

// Probe settles a nil handshake with a dial, which a hung accept still passes.
func Probe(ctx context.Context, path string, handshake Handshake) SocketState {
	if _, err := os.Stat(path); err != nil {
		return Stopped
	}

	probeCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()

	if handshake != nil {
		if err := handshake(probeCtx, path); err != nil {
			return Stuck
		}

		return Running
	}

	conn, err := (&net.Dialer{}).DialContext(probeCtx, "unix", path)
	if err != nil {
		return Stuck
	}
	_ = conn.Close()

	return Running
}

// AwaitSocket returning early would let the build race the service and fail
// its first cache operations.
func AwaitSocket(ctx context.Context, path string, handshake Handshake, budget, interval time.Duration) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return false
		}

		if Probe(ctx, path, handshake) == Running {
			return true
		}

		time.Sleep(interval)
	}

	return Probe(ctx, path, handshake) == Running
}
