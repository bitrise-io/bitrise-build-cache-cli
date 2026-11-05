//go:build unit

package proxy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/deriveddata"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/sessions"
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
		buildRequestResolver: func(*connectionSession) (BuildRequestResult, bool) {
			calls++

			return BuildRequestResult{Hex: nextHex}, true
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
		buildRequestResolver: func(*connectionSession) (BuildRequestResult, bool) {
			return BuildRequestResult{Hex: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, true
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
		buildRequestResolver: func(*connectionSession) (BuildRequestResult, bool) {
			return BuildRequestResult{Hex: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, true
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
		buildRequestResolver: func(*connectionSession) (BuildRequestResult, bool) {
			return BuildRequestResult{}, false
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
		buildRequestResolver: func(*connectionSession) (BuildRequestResult, bool) {
			return BuildRequestResult{Hex: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, true
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

// TestResolveAndMaybeReseat_FlushesOldInvocationIDSidecar pins the reseat
// flush: before the swap, any accumulated stats under the old InvocationID
// must land on a sidecar attributed to that old ID, and the per-conn state
// must then be reset so the new ⌘B starts clean.
func TestResolveAndMaybeReseat_FlushesOldInvocationIDSidecar(t *testing.T) {
	dir := t.TempDir()
	h := &sidecarStatsHandler{
		writer:               newSidecarWriter(dir, nil),
		wrapperSessionActive: func() bool { return false },
		buildRequestResolver: func(*connectionSession) (BuildRequestResult, bool) {
			return BuildRequestResult{Hex: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, true
		},
	}

	cs := newConnectionSession("k", 10, time.Now())
	cs.ancestryEntries = ideAncestry()
	cs.derivedInvocationID = h.derivePeerInvocationID(cs.ancestryEntries, "")
	oldID := cs.derivedInvocationID
	require.NotEmpty(t, oldID)

	cs.state.Load().recordDownload(opGet, 42, 0)
	require.False(t, cs.state.Load().getStats().blobStats.IsEmpty(), "test precondition: state seeded with a download")

	require.True(t, h.ResolveAndMaybeReseat(cs), "fresh hex triggers reseat")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "reseat must write exactly one intermediate sidecar for the old ID")

	body, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	require.NoError(t, err)

	var sc sessions.Sidecar
	require.NoError(t, json.Unmarshal(body, &sc))
	require.NotNil(t, sc.WrapperSession)
	assert.Equal(t, oldID, sc.WrapperSession.InvocationID, "sidecar carries the pre-reseat InvocationID, not the newly-derived one")
	assert.False(t, sc.Terminal, "reseat flush is intermediate, not terminal")

	assert.True(t, cs.state.Load().getStats().blobStats.IsEmpty(), "post-reseat state must be fresh so the new ⌘B starts clean")
	assert.NotEqual(t, oldID, cs.invocationID(), "post-reseat InvocationID is the newly-derived one")
}

// TestResolveAndMaybeReseat_FlushedSidecarCarriesOldHex pins the hex-bridge
// prerequisite: after the swap, the flushed sidecar's BuildRequestHex must be
// the pre-swap hex so the enricher can key an exact match against the
// manifest row recorded under that hex. The newly-polled hex is for the NEXT
// ⌘B — storing it on the old row would mis-attribute which manifest contains
// which blob stats.
func TestResolveAndMaybeReseat_FlushedSidecarCarriesOldHex(t *testing.T) {
	dir := t.TempDir()
	const oldHex = "11111111111111111111111111111111"
	const newHex = "22222222222222222222222222222222"

	h := &sidecarStatsHandler{
		writer:               newSidecarWriter(dir, nil),
		wrapperSessionActive: func() bool { return false },
		buildRequestResolver: func(*connectionSession) (BuildRequestResult, bool) {
			return BuildRequestResult{Hex: newHex}, true
		},
	}

	cs := newConnectionSession("k", 10, time.Now())
	cs.ancestryEntries = ideAncestry()
	cs.derivedInvocationID = h.derivePeerInvocationID(cs.ancestryEntries, oldHex)
	cs.buildRequestHex = oldHex

	cs.state.Load().recordDownload(opGet, 42, 0)

	require.True(t, h.ResolveAndMaybeReseat(cs))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	body, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	require.NoError(t, err)

	var sc sessions.Sidecar
	require.NoError(t, json.Unmarshal(body, &sc))
	assert.Equal(t, oldHex, sc.BuildRequestHex, "flushed sidecar must carry the pre-swap hex — the newly-polled one belongs to the next ⌘B")
	assert.Equal(t, newHex, cs.buildRequestHexSnapshot(), "post-reseat current hex is the newly-polled one")
}

// TestResolveAndMaybeReseat_NoFlushWhenStateEmpty guards the empty-state
// path: a reseat on a connection that never recorded anything must not
// churn the disk with an empty sidecar.
func TestResolveAndMaybeReseat_NoFlushWhenStateEmpty(t *testing.T) {
	dir := t.TempDir()
	h := &sidecarStatsHandler{
		writer:               newSidecarWriter(dir, nil),
		wrapperSessionActive: func() bool { return false },
		buildRequestResolver: func(*connectionSession) (BuildRequestResult, bool) {
			return BuildRequestResult{Hex: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, true
		},
	}

	cs := newConnectionSession("k", 10, time.Now())
	cs.ancestryEntries = ideAncestry()
	cs.derivedInvocationID = h.derivePeerInvocationID(cs.ancestryEntries, "")

	require.True(t, h.ResolveAndMaybeReseat(cs))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "empty-state reseat must not produce a sidecar file")
}

// TestResolveAndMaybeReseat_StampsFreshBuildStartedAtAfterOldSnapshot pins the
// per-build start precedence on reseat: the OLD buildStartedAt is snapshot on
// the flushed sidecar BEFORE the swap, and the connection's current
// buildStartedAt advances to a FRESH time AFTER the swap. Three ticks → three
// distinct start times (initial + two reseats).
func TestResolveAndMaybeReseat_StampsFreshBuildStartedAtAfterOldSnapshot(t *testing.T) {
	dir := t.TempDir()
	nextHex := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	h := &sidecarStatsHandler{
		writer:               newSidecarWriter(dir, nil),
		wrapperSessionActive: func() bool { return false },
		buildRequestResolver: func(*connectionSession) (BuildRequestResult, bool) {
			return BuildRequestResult{Hex: nextHex}, true
		},
	}

	cs := newConnectionSession("k", 10, time.Now())
	cs.ancestryEntries = ideAncestry()
	cs.derivedInvocationID = h.derivePeerInvocationID(cs.ancestryEntries, "")
	// Simulate TagConn's stamp: initial per-build start before any reseat.
	initialStart := time.Now().Add(-10 * time.Second)
	cs.setBuildStartedAt(initialStart)

	// Seed stats so the reseat flush actually writes a sidecar.
	cs.state.Load().recordDownload(opGet, 42, 0)

	// Tick 1: fresh hex → reseat. Old sidecar carries initialStart; connection
	// advances to a fresh start.
	require.True(t, h.ResolveAndMaybeReseat(cs))
	afterTick1 := cs.buildStartedAtSnapshot()
	assert.True(t, afterTick1.After(initialStart), "reseat must advance buildStartedAt to a fresh time")

	// Tick 2: hex advances again → reseat again, fresh stats first.
	cs.state.Load().recordDownload(opGet, 43, 0)
	nextHex = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	require.True(t, h.ResolveAndMaybeReseat(cs))
	afterTick2 := cs.buildStartedAtSnapshot()
	assert.True(t, afterTick2.After(afterTick1), "second reseat must advance buildStartedAt again")

	// Verify the two flushed sidecars carry the OLD buildStartedAt — not the fresh one.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2, "two reseats with seeded stats produce two intermediate sidecars")

	// Collect the sidecar BuildStartedAt values; they must match {initialStart, afterTick1}
	// (in UTC, with omitempty-safe serialisation), NOT the fresh afterTick2.
	seen := make(map[string]struct{})
	for _, de := range entries {
		body, err := os.ReadFile(filepath.Join(dir, de.Name()))
		require.NoError(t, err)

		var sc sessions.Sidecar
		require.NoError(t, json.Unmarshal(body, &sc))
		require.False(t, sc.BuildStartedAt.IsZero(), "sidecar must carry a non-zero BuildStartedAt")
		assert.True(t, sc.BuildStartedAt.Before(afterTick2), "sidecar carries pre-reseat start, not the fresh one")
		seen[sc.BuildStartedAt.UTC().String()] = struct{}{}
	}
	assert.Len(t, seen, 2, "two reseats observe two distinct OLD start times (initial + tick1)")
}

func TestPollAllForReseat_IteratesEveryRegisteredSession(t *testing.T) {
	registry := newSessionRegistry()
	h := &sidecarStatsHandler{
		registry:             registry,
		wrapperSessionActive: func() bool { return false },
		buildRequestResolver: func(*connectionSession) (BuildRequestResult, bool) {
			return BuildRequestResult{Hex: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, true
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

// TestResolveAndMaybeReseat_StashesBuildRequestFromResolver pins the S6 wiring:
// the resolver now returns a parsed BuildRequest alongside the hex, and
// ResolveAndMaybeReseat must stash the new classification on the session after
// the swap so terminal / idle flushes serialise it onto downstream sidecars.
func TestResolveAndMaybeReseat_StashesBuildRequestFromResolver(t *testing.T) {
	want := deriveddata.BuildRequest{
		Command:       "prepareForIndexing",
		Action:        "indexbuild",
		Configuration: "Debug",
		Container:     "IceCubesApp",
	}

	h := &sidecarStatsHandler{
		wrapperSessionActive: func() bool { return false },
		buildRequestResolver: func(*connectionSession) (BuildRequestResult, bool) {
			return BuildRequestResult{Hex: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Request: want}, true
		},
	}

	cs := newConnectionSession("k", 10, time.Now())
	cs.ancestryEntries = ideAncestry()
	cs.derivedInvocationID = h.derivePeerInvocationID(cs.ancestryEntries, "")

	require.True(t, h.ResolveAndMaybeReseat(cs))
	assert.Equal(t, want, cs.buildRequestSnapshot(), "post-reseat session must carry the parsed BuildRequest")
}

// TestResolveAndMaybeReseat_FlushedSidecarCarriesOldClassification pins the
// OLD-before-swap snapshot: on a reseat, the flushed sidecar must serialise
// the pre-swap classification so BE rows attribute the pre-reseat ⌘B's blob
// stats to its own classification (not the freshly-polled one that belongs to
// the next ⌘B).
func TestResolveAndMaybeReseat_FlushedSidecarCarriesOldClassification(t *testing.T) {
	dir := t.TempDir()
	newClass := deriveddata.BuildRequest{Command: "build", Container: "NextApp"}

	h := &sidecarStatsHandler{
		writer:               newSidecarWriter(dir, nil),
		wrapperSessionActive: func() bool { return false },
		buildRequestResolver: func(*connectionSession) (BuildRequestResult, bool) {
			return BuildRequestResult{Hex: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Request: newClass}, true
		},
	}

	cs := newConnectionSession("k", 10, time.Now())
	cs.ancestryEntries = ideAncestry()
	cs.derivedInvocationID = h.derivePeerInvocationID(cs.ancestryEntries, "")
	cs.buildRequestHex = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cs.setBuildRequest(deriveddata.BuildRequest{Command: "prepareForIndexing", Container: "PrevApp"})

	// Seed stats so the reseat flush actually writes a sidecar.
	cs.state.Load().recordDownload(opGet, 42, 0)

	require.True(t, h.ResolveAndMaybeReseat(cs))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "reseat with seeded stats must write one intermediate sidecar")

	body, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	require.NoError(t, err)

	var sc sessions.Sidecar
	require.NoError(t, json.Unmarshal(body, &sc))
	require.NotNil(t, sc.BuildRequest, "sidecar must serialise the classification carried on the OLD identity")
	assert.Equal(t, "prepareForIndexing", sc.BuildRequest.Command)
	assert.Equal(t, "PrevApp", sc.BuildRequest.Container)

	assert.Equal(t, newClass, cs.buildRequestSnapshot(), "post-reseat session carries the newly-polled classification")
}
