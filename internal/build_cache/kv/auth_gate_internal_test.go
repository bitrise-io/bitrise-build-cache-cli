//go:build unit

package kv

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type countingLogger struct {
	log.Logger
	warns atomic.Int64
}

func (c *countingLogger) Warnf(_ string, _ ...any) { c.warns.Add(1) }

func TestAuthGate_LogsOnceThenStaysBroken(t *testing.T) {
	lg := &countingLogger{Logger: log.NewLogger()}
	g := &authGate{logger: lg}

	require.False(t, g.isBroken())

	assert.True(t, g.tripOnce(status.Error(codes.Unauthenticated, "bad token")))
	assert.True(t, g.tripOnce(status.Error(codes.Unauthenticated, "still bad")))
	assert.True(t, g.tripOnce(ErrCacheUnauthenticated))

	assert.True(t, g.isBroken())
	assert.Equal(t, int64(1), lg.warns.Load(), "auth-broken warning must fire exactly once for the process")
}

func TestAuthGate_TransientErrorDoesNotTrip(t *testing.T) {
	lg := &countingLogger{Logger: log.NewLogger()}
	g := &authGate{logger: lg}

	assert.False(t, g.tripOnce(status.Error(codes.Unavailable, "later")))
	assert.False(t, g.tripOnce(errors.New("connection reset")))
	assert.False(t, g.isBroken())
	assert.Zero(t, lg.warns.Load(), "transient network blips must not disable the cache")
}

// A trip with no logger set must not latch the sync.Once — otherwise the first
// warning is lost forever when NewClient constructs with a nil logger and
// SetLogger installs one after the first trip.
func TestAuthGate_LatchesOnFirstLoggableTrip(t *testing.T) {
	g := &authGate{}

	assert.True(t, g.tripOnce(status.Error(codes.Unauthenticated, "bad token")))
	require.True(t, g.isBroken())

	lg := &countingLogger{Logger: log.NewLogger()}
	g.logger = lg

	assert.True(t, g.tripOnce(status.Error(codes.Unauthenticated, "still bad")))
	assert.Equal(t, int64(1), lg.warns.Load(), "the first trip with a logger attached must produce the single warning")

	assert.True(t, g.tripOnce(status.Error(codes.Unauthenticated, "yet again")))
	assert.Equal(t, int64(1), lg.warns.Load(), "subsequent trips must stay silent")
}

func TestAuthGate_TripsOnNonPrintableHeaderRejection(t *testing.T) {
	lg := &countingLogger{Logger: log.NewLogger()}
	g := &authGate{logger: lg}

	// grpc/internal/metadata rejects a non-printable value with this exact
	// wording; it never surfaces as a status code.
	wrapped := fmt.Errorf(`send data: header key "authorization" contains value with %s`, grpcNonPrintableHeaderMsg)

	assert.True(t, g.tripOnce(wrapped))
	assert.True(t, g.isBroken())
	assert.Equal(t, int64(1), lg.warns.Load())
}

// Covers every RPC entry point: once the gate is broken they must all return
// ErrCacheUnauthenticated without hitting the transport and without re-logging.
func TestClient_ShortCircuitsAfterAuthBroken(t *testing.T) {
	lg := &countingLogger{Logger: log.NewLogger()}
	c := &Client{logger: lg, authGate: authGate{logger: lg}}

	c.authGate.tripOnce(status.Error(codes.Unauthenticated, "bad"))
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
