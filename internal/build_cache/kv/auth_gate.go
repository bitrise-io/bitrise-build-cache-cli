package kv

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/bitrise-io/go-utils/v2/log"
)

// grpcNonPrintableHeaderMsg is the exact wording grpc/internal/metadata uses
// when it rejects a header value containing bytes outside 0x20..0x7E. It never
// surfaces as a gRPC status — matching the message is the only signal.
const grpcNonPrintableHeaderMsg = "non-printable ASCII characters"

// authGate short-circuits the client after the backend has decisively rejected
// the token. A rejected token will not be accepted on a retry; keeping the
// warning stream open only floods logs while silently disabling the cache. Once
// broken, every RPC returns ErrCacheUnauthenticated immediately so the process
// stops burning its retry budget on a token that will never be accepted.
type authGate struct {
	broken atomic.Bool
	logged sync.Once
	logger log.Logger
}

// tripOnce sets the broken flag if err looks like a token rejection and emits a
// single actionable log line. Returns true when the gate is now broken (either
// this call tripped it or a previous one did).
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

// isAuthReject covers both categories the gate reacts to: backend
// Unauthenticated (gRPC status or our sentinel) and the client-side gRPC
// metadata check for non-printable header bytes — the latter never even leaves
// the process, so it never surfaces as a status code.
func isAuthReject(err error) bool {
	if err == nil {
		return false
	}
	if isUnauthenticated(err) {
		return true
	}

	return strings.Contains(err.Error(), grpcNonPrintableHeaderMsg)
}
