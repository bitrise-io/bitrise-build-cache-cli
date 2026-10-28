//go:build unit

package proxy

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv"
)

func TestContextWithSession_TagsCtxWithDerivedInvocationID(t *testing.T) {
	cs := newConnectionSession("peer-key", 1234, time.Now())
	cs.derivedInvocationID = "deadbeef"

	ctx := contextWithSession(context.Background(), cs)

	got, ok := kv.InvocationIDFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, "deadbeef", got)

	// The per-conn session value must still be reachable so the stats handler
	// path remains intact.
	assert.Same(t, cs, sessionFromContext(ctx))
}

func TestContextWithSession_EmptyDerivedIDLeavesCtxUntagged(t *testing.T) {
	cs := newConnectionSession("peer-key", 1234, time.Now())
	// derivedInvocationID stays "" — wrapper owns the session or no anchor.

	ctx := contextWithSession(context.Background(), cs)

	_, ok := kv.InvocationIDFromContext(ctx)
	assert.False(t, ok,
		"an empty derived ID must not plant a ctx value — gRPC calls then fall through to the wrapper's c.invocationID")
}

func TestContextWithSession_SurvivesStandardContextWrappers(t *testing.T) {
	cs := newConnectionSession("peer-key", 1234, time.Now())
	cs.derivedInvocationID = "deadbeef"

	base := contextWithSession(context.Background(), cs)

	// Values flow through WithoutCancel + WithTimeout since both wrap the
	// parent ctx without filtering values.
	noCancel := context.WithoutCancel(base)
	withTimeout, cancel := context.WithTimeout(noCancel, time.Minute)
	defer cancel()

	got, ok := kv.InvocationIDFromContext(withTimeout)
	require.True(t, ok)
	assert.Equal(t, "deadbeef", got)
}
