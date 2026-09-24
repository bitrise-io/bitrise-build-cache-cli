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

// authGate latches on a token rejection and short-circuits RPCs with
// ErrCacheUnauthenticated while the credential still sends the rejected header.
// A rejected token is not going to be accepted on a retry, so keeping the stream
// open only floods logs; a refreshed one should be tried.
type authGate struct {
	broken atomic.Bool
	mu     sync.Mutex
	// rejected is the authorization header of the RPC that tripped the gate.
	rejected  string
	loggedFor *string
	logger    log.Logger
}

// tripOnce returns true when the gate is broken — either this call tripped it
// or a previous one did. sentAuth is the authorization header the failed RPC
// carried. Warns once per rejected header, on the first trip with a logger.
func (g *authGate) tripOnce(err error, sentAuth string) bool {
	if !isAuthReject(err) {
		return g.broken.Load()
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	g.rejected = sentAuth
	g.broken.Store(true)
	if g.logger != nil && (g.loggedFor == nil || *g.loggedFor != sentAuth) {
		g.loggedFor = &sentAuth
		g.logger.Warnf(
			"Build Cache auth rejected (%s) — disabling cache until the credential changes; check BITRISE_BUILD_CACHE_AUTH_TOKEN for trailing whitespace or expired credentials",
			err,
		)
	}

	return true
}

// isBroken reports whether current, the header the next RPC would send, is the
// rejected one. A nil current keeps the gate latched.
func (g *authGate) isBroken(current func() string) bool {
	if !g.broken.Load() {
		return false
	}
	if current == nil {
		return true
	}

	next := current()

	g.mu.Lock()
	defer g.mu.Unlock()

	if next != g.rejected {
		g.broken.Store(false)

		return false
	}

	return true
}

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
