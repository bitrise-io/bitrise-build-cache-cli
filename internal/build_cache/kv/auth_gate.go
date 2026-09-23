package kv

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/bitrise-io/go-utils/v2/log"
)

// grpcNonPrintableHeaderMsg is the exact wording grpc/internal/metadata uses
// when it rejects a header value containing bytes outside 0x20..0x7E; it never
// surfaces as a gRPC status, so matching the message is the only signal.
const grpcNonPrintableHeaderMsg = "non-printable ASCII characters"

// authGate latches on the first token rejection and short-circuits every
// subsequent RPC with ErrCacheUnauthenticated. A rejected token is not going to
// be accepted on a retry, so keeping the stream open only floods logs.
type authGate struct {
	broken atomic.Bool
	logged sync.Once
	logger log.Logger
}

// tripOnce returns true when the gate is broken — either this call tripped it
// or a previous one did. Emits a single actionable log line on the first trip
// that happens to have a logger attached (see the latch-on-first-loggable-trip
// test).
func (g *authGate) tripOnce(err error) bool {
	if !isAuthReject(err) {
		return g.broken.Load()
	}

	g.broken.Store(true)
	if g.logger != nil {
		g.logged.Do(func() {
			g.logger.Warnf(
				"Build Cache auth rejected (%s) — disabling cache for the rest of this process; check BITRISE_BUILD_CACHE_AUTH_TOKEN for trailing whitespace or expired credentials",
				err,
			)
		})
	}

	return true
}

func (g *authGate) isBroken() bool { return g.broken.Load() }

// isAuthReject covers both a backend Unauthenticated (gRPC status or our
// sentinel) and the client-side gRPC metadata check for non-printable header
// bytes — the latter never leaves the process, so it has no status code.
func isAuthReject(err error) bool {
	if err == nil {
		return false
	}
	if isUnauthenticated(err) {
		return true
	}

	return strings.Contains(err.Error(), grpcNonPrintableHeaderMsg)
}
