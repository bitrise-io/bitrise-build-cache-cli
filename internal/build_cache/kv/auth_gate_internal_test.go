//go:build unit

package kv

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

type countingLogger struct {
	log.Logger
	warns atomic.Int64
}

func (c *countingLogger) Warnf(_ string, _ ...any) { c.warns.Add(1) }

func TestAuthGate_LogsOnceThenStaysBroken(t *testing.T) {
	lg := &countingLogger{Logger: log.NewLogger()}
	g := &authGate{logger: lg}

	require.False(t, g.isBroken(nil))

	assert.True(t, g.tripOnce(status.Error(codes.Unauthenticated, "bad token"), "bearer bad", nil))
	assert.True(t, g.tripOnce(status.Error(codes.Unauthenticated, "still bad"), "bearer bad", nil))
	assert.True(t, g.tripOnce(ErrCacheUnauthenticated, "bearer bad", nil))

	assert.True(t, g.isBroken(nil))
	assert.Equal(t, int64(1), lg.warns.Load(), "auth-broken warning must fire exactly once per rejected credential")
}

func TestAuthGate_TransientErrorDoesNotTrip(t *testing.T) {
	lg := &countingLogger{Logger: log.NewLogger()}
	g := &authGate{logger: lg}

	assert.False(t, g.tripOnce(status.Error(codes.Unavailable, "later"), "bearer bad", nil))
	assert.False(t, g.tripOnce(errors.New("connection reset"), "bearer bad", nil))
	assert.False(t, g.isBroken(nil))
	assert.Zero(t, lg.warns.Load(), "transient network blips must not disable the cache")
}

// A trip with no logger set must not latch the sync.Once — otherwise the first
// warning is lost forever when NewClient constructs with a nil logger and
// SetLogger installs one after the first trip.
func TestAuthGate_LatchesOnFirstLoggableTrip(t *testing.T) {
	g := &authGate{}

	assert.True(t, g.tripOnce(status.Error(codes.Unauthenticated, "bad token"), "bearer bad", nil))
	require.True(t, g.isBroken(nil))

	lg := &countingLogger{Logger: log.NewLogger()}
	g.logger = lg

	assert.True(t, g.tripOnce(status.Error(codes.Unauthenticated, "still bad"), "bearer bad", nil))
	assert.Equal(t, int64(1), lg.warns.Load(), "the first trip with a logger attached must produce the single warning")

	assert.True(t, g.tripOnce(status.Error(codes.Unauthenticated, "yet again"), "bearer bad", nil))
	assert.Equal(t, int64(1), lg.warns.Load(), "subsequent trips must stay silent")
}

func TestAuthGate_TripsOnNonPrintableHeaderRejection(t *testing.T) {
	lg := &countingLogger{Logger: log.NewLogger()}
	g := &authGate{logger: lg}

	// grpc/internal/metadata rejects a non-printable value with this exact
	// wording; it never surfaces as a status code.
	wrapped := fmt.Errorf(`send data: header key "authorization" contains value with %s`, grpcNonPrintableHeaderMsg)

	assert.True(t, g.tripOnce(wrapped, "bearer bad", nil))
	assert.True(t, g.isBroken(nil))
	assert.Equal(t, int64(1), lg.warns.Load())
}

// Covers every RPC entry point: once the gate is broken they must all return
// ErrCacheUnauthenticated without hitting the transport and without re-logging.
func TestClient_ShortCircuitsAfterAuthBroken(t *testing.T) {
	lg := &countingLogger{Logger: log.NewLogger()}
	c := &Client{logger: lg, authGate: authGate{logger: lg}}

	c.authGate.tripOnce(status.Error(codes.Unauthenticated, "bad"), "bearer bad", nil)
	require.Equal(t, int64(1), lg.warns.Load())

	err := c.GetCapabilities(t.Context())
	require.ErrorIs(t, err, ErrCacheUnauthenticated)

	_, err = c.initiatePut(t.Context(), PutParams{})
	require.ErrorIs(t, err, ErrCacheUnauthenticated)

	_, err = c.initiateGet(t.Context(), lg, "any", 0)
	require.ErrorIs(t, err, ErrCacheUnauthenticated)

	err = c.Delete(t.Context(), "any")
	require.ErrorIs(t, err, ErrCacheUnauthenticated)

	_, err = c.QueryWriteStatus(t.Context(), "any")
	require.ErrorIs(t, err, ErrCacheUnauthenticated)

	assert.Equal(t, int64(1), lg.warns.Load(), "short-circuit path must not re-log")
}

type fixedAuth struct{ token atomic.Pointer[string] }

func (f *fixedAuth) Get(context.Context) auth.Credential { return auth.Credential{Token: *f.token.Load()} }

func TestClient_AuthGateReopensWhenTheCredentialChanges(t *testing.T) {
	lg := &countingLogger{Logger: log.NewLogger()}
	src := &fixedAuth{}
	stale := "stale"
	src.token.Store(&stale)
	c := &Client{logger: lg, authGate: authGate{logger: lg}, authSource: src}

	c.tripAuth(t.Context(), status.Error(codes.Unauthenticated, "expired"), bearer(stale))
	assert.True(t, c.authBroken(t.Context()), "the rejected credential must stay short-circuited")

	fresh := "fresh"
	src.token.Store(&fresh)
	assert.False(t, c.authBroken(t.Context()), "a refreshed credential must get another try")

	c.tripAuth(t.Context(), status.Error(codes.Unauthenticated, "also bad"), bearer(fresh))
	assert.True(t, c.authBroken(t.Context()))
	assert.Equal(t, int64(2), lg.warns.Load(), "each rejected credential warns once")
}

func TestClient_TransientErrorOfAFreshCredentialDoesNotAbort(t *testing.T) {
	lg := &countingLogger{Logger: log.NewLogger()}
	src := &fixedAuth{}
	stale := "stale"
	src.token.Store(&stale)
	c := &Client{logger: lg, authGate: authGate{logger: lg}, authSource: src}

	require.True(t, c.tripAuth(t.Context(), status.Error(codes.Unauthenticated, "expired"), bearer(stale)))
	assert.True(t, c.tripAuth(t.Context(), status.Error(codes.Unavailable, "later"), bearer(stale)), "the rejected credential stays aborted")

	fresh := "fresh"
	src.token.Store(&fresh)
	assert.False(t, c.tripAuth(t.Context(), status.Error(codes.Unavailable, "later"), bearer(fresh)), "a transient error on another credential must stay retryable")
}

func TestClient_LateRejectionOfAReplacedCredentialDoesNotRelatch(t *testing.T) {
	lg := &countingLogger{Logger: log.NewLogger()}
	src := &fixedAuth{}
	stale := "stale"
	src.token.Store(&stale)
	c := &Client{logger: lg, authGate: authGate{logger: lg}, authSource: src}

	require.True(t, c.tripAuth(t.Context(), status.Error(codes.Unauthenticated, "expired"), bearer(stale)))

	fresh := "fresh"
	src.token.Store(&fresh)
	require.False(t, c.authBroken(t.Context()))

	assert.False(t, c.tripAuth(t.Context(), status.Error(codes.Unauthenticated, "expired"), bearer(stale)), "the retry sends the new credential")
	assert.False(t, c.authBroken(t.Context()))

	require.True(t, c.tripAuth(t.Context(), status.Error(codes.Unauthenticated, "also bad"), bearer(fresh)))
	assert.False(t, c.tripAuth(t.Context(), status.Error(codes.Unauthenticated, "expired"), bearer(stale)))
	assert.True(t, c.authBroken(t.Context()), "a late stale rejection must not overwrite the fresh one")
	assert.Equal(t, int64(2), lg.warns.Load())
}

func TestClient_SetLoggerDoesNotRaceATrip(t *testing.T) {
	c := &Client{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.authGate.tripOnce(status.Error(codes.Unauthenticated, "bad"), "bearer bad", nil)
	}()
	c.SetLogger(log.NewLogger())
	<-done
}
