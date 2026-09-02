package ccache

import (
	"context"
	"os"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/spawn"
)

// ProbeSocket reports whether the storage helper answers on path. It completes
// the health-check handshake instead of dialing and closing, which the helper
// logs as "Capabilities check failed" — CI asserts on those lines.
func ProbeSocket(ctx context.Context, path string) spawn.SocketState {
	if _, err := os.Stat(path); err != nil {
		return spawn.Stopped
	}

	checkCtx, cancel := context.WithTimeout(ctx, spawn.ProbeTimeout)
	defer cancel()

	if err := SendHealthCheck(checkCtx, path); err != nil {
		return spawn.Stuck
	}

	return spawn.Running
}

// AwaitSocket polls ProbeSocket until the helper answers or the budget runs
// out. Returning early lets ccache race the helper and miss its first lookups.
func AwaitSocket(ctx context.Context, path string, budget, interval time.Duration) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return false
		}

		if ProbeSocket(ctx, path) == spawn.Running {
			return true
		}

		time.Sleep(interval)
	}

	return ProbeSocket(ctx, path) == spawn.Running
}
