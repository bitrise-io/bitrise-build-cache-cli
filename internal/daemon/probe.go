package daemon

import (
	"context"
	"net"
	"os"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/ccache"
)

// SocketProbe classifies a unix-socket-backed service as running, stopped, or
// stuck. Shared by `daemon info` (human-facing status) and `Ensure` (decides
// whether to restart on push-flag change).
type SocketProbe int

const (
	// ProbeRunning: socket accepts connections (or, for ccache, replies OK to a
	// health-check handshake).
	ProbeRunning SocketProbe = iota
	// ProbeStopped: socket file is absent — the supervisor is not holding it open.
	ProbeStopped
	// ProbeStuck: socket file exists but a dial fails — leftover from a crashed
	// helper; the supervisor is not currently bound.
	ProbeStuck
)

// ProbeTimeout is the dial/handshake budget for a single probe.
const ProbeTimeout = 500 * time.Millisecond

// ProbeSocket dials a unix socket and reports whether something is listening.
// A hung accept still counts as running — the caller only wants to know if the
// supervisor is holding the port. Stat errors (including ENOENT) map to
// ProbeStopped; a successful stat followed by a failed dial maps to ProbeStuck.
// ctx is used for the dial only; the stat is always synchronous.
func ProbeSocket(ctx context.Context, path string) SocketProbe {
	if _, err := os.Stat(path); err != nil {
		return ProbeStopped
	}

	dialCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(dialCtx, "unix", path)
	if err != nil {
		return ProbeStuck
	}
	_ = conn.Close()

	return ProbeRunning
}

// ProbeCcacheSocket uses the ccache protocol's health-check exchange so the
// storage helper sees a clean handshake — a raw dial+close would surface as
// "Capabilities check failed" in the helper's log, and CI asserts on those.
func ProbeCcacheSocket(ctx context.Context, path string) SocketProbe {
	if _, err := os.Stat(path); err != nil {
		return ProbeStopped
	}

	checkCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()

	if err := ccache.SendHealthCheck(checkCtx, path); err != nil {
		return ProbeStuck
	}

	return ProbeRunning
}

// ProbeFn probes a given service. Ensure needs one — the daemon package cannot
// resolve per-tool socket paths itself without importing the config packages
// (which would form a cycle with the callers of Ensure).
type ProbeFn func(ctx context.Context, svc Service) SocketProbe
