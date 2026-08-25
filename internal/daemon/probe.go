package daemon

import (
	"context"
	"net"
	"os"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/ccache"
)

type SocketProbe int

const (
	ProbeRunning SocketProbe = iota
	ProbeStopped
	ProbeStuck
)

const ProbeTimeout = 500 * time.Millisecond

// ProbeSocket dials a unix socket. A hung accept still counts as running.
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

// ProbeCcacheSocket runs the ccache health-check handshake instead of a raw
// dial+close, which would surface as "Capabilities check failed" in the
// helper's log — CI asserts on those.
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
