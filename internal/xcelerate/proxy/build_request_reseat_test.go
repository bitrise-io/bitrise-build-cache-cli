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

// ideAncestry returns an ancestry chain that passes isIDEAncestry: the IDE
// anchor is SWBBuildService, with no xcodebuild entry.
func ideAncestry() []AncestryEntry {
	return []AncestryEntry{
		{PID: 10, Name: "swiftc", StartTimeMS: 1},
		{PID: 11, Name: "SWBBuildService", StartTimeMS: 2},
		{PID: 12, Name: "Xcode", StartTimeMS: 3},
	}
}

// cliAncestry returns an ancestry chain for an xcodebuild CLI run — must NOT
// be reseated (its InvocationID is stable per process already).
func cliAncestry() []AncestryEntry {
	return []AncestryEntry{
		{PID: 20, Name: "swiftc", StartTimeMS: 1},
		{PID: 21, Name: "xcodebuild", StartTimeMS: 2},
	}
}

func TestResolveAndMaybeReseat_IDEEdgeTriggersReseat(t *testing.T) {
	calls := 0
	nextHex := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	h := &sidecarStatsHandler{
		wrapperSessionActive: func() bool { return false },
		buildRequestResolver: func(*connectionSession) (string, bool) {
			calls++

			return nextHex, true
		},
	}

	cs := newConnectionSession("k", 10, time.Now())
	cs.ancestryEntries = ideAncestry()
	cs.derivedInvocationID = h.derivePeerInvocationID(cs.ancestryEntries, "")
	initial := cs.derivedInvocationID
	require.NotEmpty(t, initial, "IDE anchor must derive an initial InvocationID")

	// Tick 1: fresh hex → reseat, ID changes.
	assert.True(t, h.ResolveAndMaybeReseat(cs))
	reseated := cs.invocationID()
	assert.NotEqual(t, initial, reseated, "initial reseat must change the ID")
	assert.NotEmpty(t, reseated)

	// Tick 2: same hex → no reseat, ID unchanged.
	assert.False(t, h.ResolveAndMaybeReseat(cs))
	assert.Equal(t, reseated, cs.invocationID())

	// Tick 3: hex advances → reseat again, ID changes once more.
	nextHex = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	assert.True(t, h.ResolveAndMaybeReseat(cs))
	assert.NotEqual(t, reseated, cs.invocationID())

	assert.Equal(t, 3, calls, "resolver called once per tick")
}

func TestResolveAndMaybeReseat_CLIAncestryNoReseat(t *testing.T) {
	h := &sidecarStatsHandler{
		wrapperSessionActive: func() bool { return false },
		buildRequestResolver: func(*connectionSession) (string, bool) {
			return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true
		},
	}

	cs := newConnectionSession("k", 20, time.Now())
	cs.ancestryEntries = cliAncestry()
	cs.derivedInvocationID = h.derivePeerInvocationID(cs.ancestryEntries, "")
	initial := cs.derivedInvocationID
	require.NotEmpty(t, initial)

	assert.False(t, h.ResolveAndMaybeReseat(cs), "CLI (xcodebuild) ancestry must not reseat")
	assert.Equal(t, initial, cs.invocationID())
}

func TestResolveAndMaybeReseat_WrapperSessionWinsOverReseat(t *testing.T) {
	h := &sidecarStatsHandler{
		wrapperSessionActive: func() bool { return true },
		buildRequestResolver: func(*connectionSession) (string, bool) {
			return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true
		},
	}

	cs := newConnectionSession("k", 10, time.Now())
	cs.ancestryEntries = ideAncestry()
	// derivation under wrapper-active returns "" by design; mirror that here.
	cs.derivedInvocationID = ""

	assert.False(t, h.ResolveAndMaybeReseat(cs), "wrapper session must suppress reseat")
	assert.Empty(t, cs.invocationID())
}

func TestResolveAndMaybeReseat_NilResolverNoOp(t *testing.T) {
	h := &sidecarStatsHandler{
		wrapperSessionActive: func() bool { return false },
	}

	cs := newConnectionSession("k", 10, time.Now())
	cs.ancestryEntries = ideAncestry()
	cs.derivedInvocationID = h.derivePeerInvocationID(cs.ancestryEntries, "")
	initial := cs.derivedInvocationID

	assert.False(t, h.ResolveAndMaybeReseat(cs))
	assert.Equal(t, initial, cs.invocationID(), "nil resolver must leave the ID untouched")
}

func TestResolveAndMaybeReseat_EmptyHexResultNoOp(t *testing.T) {
	h := &sidecarStatsHandler{
		wrapperSessionActive: func() bool { return false },
		buildRequestResolver: func(*connectionSession) (string, bool) {
			return "", false
		},
	}

	cs := newConnectionSession("k", 10, time.Now())
	cs.ancestryEntries = ideAncestry()
	cs.derivedInvocationID = h.derivePeerInvocationID(cs.ancestryEntries, "")
	initial := cs.derivedInvocationID

	assert.False(t, h.ResolveAndMaybeReseat(cs))
	assert.Equal(t, initial, cs.invocationID())
}

func TestIsIDEAncestry_Classification(t *testing.T) {
	cases := []struct {
		name    string
		entries []AncestryEntry
		want    bool
	}{
		{
			name:    "pure IDE chain",
			entries: ideAncestry(),
			want:    true,
		},
		{
			name:    "xcodebuild CLI chain",
			entries: cliAncestry(),
			want:    false,
		},
		{
			name: "xcodebuild wins over Xcode (CLI wrapped by Xcode is still CLI semantics)",
			entries: []AncestryEntry{
				{PID: 1, Name: "swiftc"},
				{PID: 2, Name: "xcodebuild"},
				{PID: 3, Name: "Xcode"},
			},
			want: false,
		},
		{
			name:    "unrelated chain",
			entries: []AncestryEntry{{PID: 1, Name: "someRandomProbe"}},
			want:    false,
		},
		{
			name:    "empty chain",
			entries: nil,
			want:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isIDEAncestry(tc.entries))
		})
	}
}

// TestRewrapInvocationID_PicksUpReseatedID is the key end-to-end assertion
// for the mid-connection reseat: once ResolveAndMaybeReseat flips the per-conn
// derivedInvocationID, the next RPC's ctx (re-wrapped by the unary interceptor)
// carries the new ID to the KV layer. Without this bridge, the KV ctx value
// planted at TagConn would freeze on the pre-reseat ID.
func TestRewrapInvocationID_PicksUpReseatedID(t *testing.T) {
	h := &sidecarStatsHandler{
		wrapperSessionActive: func() bool { return false },
		buildRequestResolver: func(*connectionSession) (string, bool) {
			return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true
		},
	}

	cs := newConnectionSession("k", 10, time.Now())
	cs.ancestryEntries = ideAncestry()
	cs.derivedInvocationID = h.derivePeerInvocationID(cs.ancestryEntries, "")
	initial := cs.derivedInvocationID

	// Build the TagConn-equivalent ctx: carries session + initial InvocationID.
	ctx := contextWithSession(context.Background(), cs)

	got, ok := kv.InvocationIDFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, initial, got, "ctx baseline carries the initial derivation")

	// Reseat (as a tick would do) and re-wrap — mimics the unary interceptor.
	require.True(t, h.ResolveAndMaybeReseat(cs))
	rewrapped := rewrapInvocationID(ctx)

	got, ok = kv.InvocationIDFromContext(rewrapped)
	require.True(t, ok)
	assert.NotEqual(t, initial, got, "rewrap must pick up the reseated ID")
	assert.Equal(t, cs.invocationID(), got)
}

// TestRewrapInvocationID_NoSessionPassThrough covers the degenerate case the
// interceptor also hits during bufconn-style tests: ctx without a per-conn
// session must pass through unchanged.
func TestRewrapInvocationID_NoSessionPassThrough(t *testing.T) {
	ctx := context.Background()
	got := rewrapInvocationID(ctx)

	assert.Equal(t, ctx, got)
	_, ok := kv.InvocationIDFromContext(got)
	assert.False(t, ok)
}

func TestPollAllForReseat_IteratesEveryRegisteredSession(t *testing.T) {
	registry := newSessionRegistry()
	h := &sidecarStatsHandler{
		registry:             registry,
		wrapperSessionActive: func() bool { return false },
		buildRequestResolver: func(*connectionSession) (string, bool) {
			return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true
		},
	}

	// Two IDE sessions — both must reseat.
	for i := 0; i < 2; i++ {
		cs := newConnectionSession(keyAt(i), 100+i, time.Now())
		cs.ancestryEntries = ideAncestry()
		cs.derivedInvocationID = h.derivePeerInvocationID(cs.ancestryEntries, "")
		registry.put(cs)
	}

	// One CLI session — must not reseat.
	cs := newConnectionSession(keyAt(2), 200, time.Now())
	cs.ancestryEntries = cliAncestry()
	cs.derivedInvocationID = h.derivePeerInvocationID(cs.ancestryEntries, "")
	registry.put(cs)

	assert.Equal(t, 2, h.pollAllForReseat(), "only the two IDE sessions reseat")
}
