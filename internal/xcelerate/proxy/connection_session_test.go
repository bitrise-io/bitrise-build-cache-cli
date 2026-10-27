//go:build unit

package proxy

import (
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionRegistry_PutGetForget(t *testing.T) {
	r := newSessionRegistry()

	cs1 := newConnectionSession("k1", 100, time.Now())
	cs2 := newConnectionSession("k2", 200, time.Now())

	r.put(cs1)
	r.put(cs2)

	assert.Equal(t, 2, registryLen(r))

	r.forget("k1")
	assert.Equal(t, 1, registryLen(r))
}

func TestSessionRegistry_ConcurrentPut(t *testing.T) {
	r := newSessionRegistry()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			r.put(newConnectionSession(keyAt(i), i, time.Now()))
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 100, registryLen(r))
}

func registryLen(r *sessionRegistry) int {
	n := 0
	r.flushAll(func(*connectionSession) { n++ })

	return n
}

func TestSessionRegistry_FlushAllInvokesEveryOutstandingSession(t *testing.T) {
	r := newSessionRegistry()

	for i := 0; i < 5; i++ {
		r.put(newConnectionSession(keyAt(i), i, time.Now()))
	}

	var mu sync.Mutex
	seen := make(map[*connectionSession]struct{})

	r.flushAll(func(cs *connectionSession) {
		mu.Lock()
		defer mu.Unlock()

		seen[cs] = struct{}{}
	})

	assert.Len(t, seen, 5)
}

func keyAt(i int) string {
	return "k-" + string(rune('A'+i))
}

func TestFlushSession_SkipsWhenConnectionHadNoActivity(t *testing.T) {
	dir := t.TempDir()
	h := &sidecarStatsHandler{
		writer:           newSidecarWriter(dir, nil),
		ancestryResolver: func(int) []string { return nil },
	}

	cs := newConnectionSession("k", 42, time.Now())
	h.flushSession(cs)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "zero-RPC connection must not produce a sidecar file")
}

func TestFlushSession_WritesWhenConnectionHadActivity(t *testing.T) {
	dir := t.TempDir()
	h := &sidecarStatsHandler{
		writer:           newSidecarWriter(dir, nil),
		ancestryResolver: func(int) []string { return nil },
	}

	cs := newConnectionSession("k", 42, time.Now())
	cs.markActivity()
	h.flushSession(cs)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "a conn that recorded an RPC must emit a sidecar")
}

func TestDerivePeerInvocationID_PeerAncestryGate(t *testing.T) {
	h := &sidecarStatsHandler{
		wrapperSessionActive: func() bool { return false },
	}

	entries := []AncestryEntry{{PID: 100, Name: "someRandomProbe", StartTimeMS: 1}}
	assert.Empty(t, h.derivePeerInvocationID(entries), "no anchorable ancestor must yield empty InvocationID")
}

func TestDerivePeerInvocationID_WrapperSessionWins(t *testing.T) {
	h := &sidecarStatsHandler{
		wrapperSessionActive: func() bool { return true },
	}

	entries := []AncestryEntry{{PID: 101, Name: "xcodebuild", StartTimeMS: 2}}
	assert.Empty(t, h.derivePeerInvocationID(entries), "wrapper-owned session must suppress derivation")
}

func TestDerivePeerInvocationID_ReturnsDeterministicIDOnAnchor(t *testing.T) {
	h := &sidecarStatsHandler{
		wrapperSessionActive: func() bool { return false },
	}

	entries := []AncestryEntry{{PID: 101, Name: "xcodebuild", StartTimeMS: 1_700_000_000_000}}
	first := h.derivePeerInvocationID(entries)
	second := h.derivePeerInvocationID(entries)
	assert.NotEmpty(t, first)
	assert.Equal(t, first, second)
}

func TestTagConn_PopulatesDerivedInvocationID(t *testing.T) {
	dir := t.TempDir()
	h := &sidecarStatsHandler{
		writer:           newSidecarWriter(dir, nil),
		ancestryResolver: func(int) []string { return []string{"swiftc", "xcodebuild"} },
		peerAncestryEntries: func(int) []AncestryEntry {
			return []AncestryEntry{{PID: 42, Name: "xcodebuild", StartTimeMS: 1}}
		},
		wrapperSessionActive: func() bool { return false },
	}

	cs := newConnectionSession("k", 42, time.Now())
	cs.ancestryEntries = h.resolveEntries(42)
	cs.derivedInvocationID = h.derivePeerInvocationID(cs.ancestryEntries)
	cs.markActivity()
	h.flushSession(cs)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

// TestTagConn_WrapperSessionGate_SetAfterAccept guards the gate's capture-at-TagConn
// semantics: a wrapper SetSession that fires AFTER derivation runs must not retroactively
// clear the derived InvocationID. The sidecar still carries the derivation; the
// wrapper's own PUT at emit time is what wins for correlation (owned elsewhere by
// p.sessionState/flushSession in the invocation_emit path).
func TestTagConn_WrapperSessionGate_SetAfterAccept(t *testing.T) {
	dir := t.TempDir()
	var active atomic.Bool
	h := &sidecarStatsHandler{
		writer:           newSidecarWriter(dir, nil),
		ancestryResolver: func(int) []string { return []string{"swiftc", "xcodebuild"} },
		peerAncestryEntries: func(int) []AncestryEntry {
			return []AncestryEntry{{PID: 42, Name: "xcodebuild", StartTimeMS: 1_700_000_000_000}}
		},
		wrapperSessionActive: func() bool { return active.Load() },
	}

	// Phase 1: TagConn-equivalent — wrapper not yet active, derivation runs.
	cs := newConnectionSession("k", 42, time.Now())
	cs.ancestryEntries = h.resolveEntries(42)
	cs.derivedInvocationID = h.derivePeerInvocationID(cs.ancestryEntries)
	require.NotEmpty(t, cs.derivedInvocationID, "derivation must run when wrapper not yet active")
	derived := cs.derivedInvocationID

	// Phase 2: wrapper fires SetSession AFTER TagConn. Gate flips, but the
	// per-conn state was captured at Phase 1 and must not change.
	active.Store(true)
	assert.Equal(t, derived, cs.derivedInvocationID, "late SetSession must not retroactively clear derivation")

	cs.markActivity()
	h.flushSession(cs)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "sidecar still emitted; wrapper's own PUT wins separately via SessionState")
}
