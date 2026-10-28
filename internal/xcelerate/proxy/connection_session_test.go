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
		writer: newSidecarWriter(dir, nil),
	}

	cs := newConnectionSession("k", 42, time.Now())
	h.flushTerminal(cs)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "zero-RPC connection must not produce a sidecar file")
}

func TestFlushSession_WritesWhenConnectionHadActivity(t *testing.T) {
	dir := t.TempDir()
	h := &sidecarStatsHandler{
		writer: newSidecarWriter(dir, nil),
	}

	cs := newConnectionSession("k", 42, time.Now())
	cs.markActivity()
	h.flushTerminal(cs)

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
		writer: newSidecarWriter(dir, nil),
		peerAncestryEntries: func(int) []AncestryEntry {
			return []AncestryEntry{{PID: 42, Name: "xcodebuild", StartTimeMS: 1}}
		},
		wrapperSessionActive: func() bool { return false },
	}

	cs := newConnectionSession("k", 42, time.Now())
	cs.ancestryEntries = h.resolveEntries(42)
	cs.derivedInvocationID = h.derivePeerInvocationID(cs.ancestryEntries)
	cs.markActivity()
	h.flushTerminal(cs)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

// TestNewSessionSidecar_PropagatesDerivedDataPath pins the sidecar envelope's
// DerivedDataPath to the per-conn field; the enrichment watcher relies on this
// to glob project-local DerivedData roots.
func TestNewSessionSidecar_PropagatesDerivedDataPath(t *testing.T) {
	cs := newConnectionSession("k", 42, time.Unix(0, 0))
	cs.derivedDataPath = "/tmp/projectDD"

	sc := newSessionSidecar(cs, cs.state.Load(), time.Unix(1, 0), true)

	assert.Equal(t, "/tmp/projectDD", sc.DerivedDataPath)
	assert.True(t, sc.Terminal, "ConnEnd path marks the sidecar terminal")
}

// TestNewSessionSidecar_PeerAncestryFromCachedEntries pins the Gap #4 fix:
// the sidecar envelope's PeerAncestry names come from the ancestry entries
// captured at TagConn time, not a re-walk at write time (which races the peer
// PID's exit and would blank out the chain).
func TestNewSessionSidecar_PeerAncestryFromCachedEntries(t *testing.T) {
	cs := newConnectionSession("k", 42, time.Unix(0, 0))
	cs.ancestryEntries = []AncestryEntry{
		{PID: 42, Name: "swiftc", StartTimeMS: 1},
		{PID: 10, Name: "xcodebuild", StartTimeMS: 2},
	}

	sc := newSessionSidecar(cs, cs.state.Load(), time.Unix(1, 0), true)

	assert.Equal(t, []string{"swiftc", "xcodebuild"}, sc.PeerAncestry)
}

// TestNewSessionSidecar_PeerAncestryNilEntriesSerialisesEmpty guards the
// defensive fallback: a connection that never resolved ancestry (e.g. bufconn
// tests, resolver not wired) must still serialise without panicking.
func TestNewSessionSidecar_PeerAncestryNilEntriesSerialisesEmpty(t *testing.T) {
	cs := newConnectionSession("k", 42, time.Unix(0, 0))
	cs.ancestryEntries = nil

	sc := newSessionSidecar(cs, cs.state.Load(), time.Unix(1, 0), true)

	assert.Empty(t, sc.PeerAncestry)
}

// TestTagConn_PopulatesDerivedDataPath exercises the DD-path flow through
// resolveDerivedDataPath into the connection session.
func TestTagConn_PopulatesDerivedDataPath(t *testing.T) {
	h := &sidecarStatsHandler{
		peerAncestryEntries: func(int) []AncestryEntry {
			return []AncestryEntry{{PID: 100, Name: "xcodebuild"}}
		},
		derivedDataPathFromEntries: func(_ []AncestryEntry) string { return "/tmp/dd" },
	}

	cs := newConnectionSession("k", 100, time.Now())
	cs.ancestryEntries = h.resolveEntries(100)
	cs.derivedDataPath = h.resolveDerivedDataPath(cs.ancestryEntries)

	assert.Equal(t, "/tmp/dd", cs.derivedDataPath)
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
		writer: newSidecarWriter(dir, nil),
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
	h.flushTerminal(cs)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "sidecar still emitted; wrapper's own PUT wins separately via SessionState")
}

// TestSwapState_ReturnsPriorAndInstallsFresh pins the atomic.Pointer swap:
// Swap returns the previously-installed state, and the next Load reads the
// fresh zero-valued one. The intermediate-flush path depends on both halves.
func TestSwapState_ReturnsPriorAndInstallsFresh(t *testing.T) {
	cs := newConnectionSession("k", 42, time.Now())
	prior := cs.state.Load()
	prior.recordDownload(opGet, 100, 0)

	old := cs.swapState()
	assert.Same(t, prior, old, "swapState must return the previously-installed pointer")
	assert.False(t, old.getStats().blobStats.IsEmpty(), "old slice still carries the recorded op")

	fresh := cs.state.Load()
	assert.NotSame(t, old, fresh, "Load after swap must return a different pointer")
	assert.True(t, fresh.getStats().blobStats.IsEmpty(), "fresh slice must start zero-valued")
}

// TestSwapState_RecordsLandInExactlyOneSliceUnderContention guards the
// in-flight-RPC invariant: concurrent recorders racing a swap must each land
// fully in exactly one of {old, new} — never partially in both.
func TestSwapState_RecordsLandInExactlyOneSliceUnderContention(t *testing.T) {
	cs := newConnectionSession("k", 42, time.Now())

	const workers = 32
	var start sync.WaitGroup
	start.Add(1)
	var done sync.WaitGroup
	done.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer done.Done()
			start.Wait()
			// Each goroutine reads the pointer once (same ordering the real
			// recorder has) and records against it. The swap racing with this
			// read must still have every record land in exactly one slice.
			s := cs.state.Load()
			s.recordDownload(opGet, 1, 0)
		}()
	}

	start.Done()
	old := cs.swapState()
	done.Wait()

	fresh := cs.state.Load()

	oldOps := old.getStats().blobStats.Download.OpCount
	newOps := fresh.getStats().blobStats.Download.OpCount
	assert.Equal(t, int64(workers), oldOps+newOps, "every recorded op must land in exactly one slice")
}

// TestHadActivityWithin covers the idle-check used by flushIdle: zero
// lastActivityAt is "not within"; a recent markActivity falls inside; a
// stale lastActivityAt (threshold crossed) falls outside.
func TestHadActivityWithin(t *testing.T) {
	cs := newConnectionSession("k", 42, time.Now())

	assert.False(t, cs.hadActivityWithin(5*time.Second, time.Now()), "zero lastActivityAt counts as not-within")

	cs.markActivity()
	assert.True(t, cs.hadActivityWithin(5*time.Second, time.Now()), "fresh markActivity falls inside threshold")

	// Simulate time having passed by advancing `now` past the threshold.
	future := time.Now().Add(1 * time.Hour)
	assert.False(t, cs.hadActivityWithin(5*time.Second, future), "stale lastActivityAt falls outside")
}

// TestFlushIdle_WritesIntermediateForIdleAndActiveSessionsOnly guards the
// per-conn idle flush: a mixed registry (no-activity, recently-active,
// idle-with-ops) must produce an intermediate sidecar for only the idle one
// that actually recorded an op.
func TestFlushIdle_WritesIntermediateForIdleAndActiveSessionsOnly(t *testing.T) {
	dir := t.TempDir()
	registry := newSessionRegistry()
	h := &sidecarStatsHandler{
		writer:   newSidecarWriter(dir, nil),
		registry: registry,
	}

	// Session A: no activity at all — must skip entirely.
	noActivity := newConnectionSession("k-noact", 1, time.Unix(0, 0))
	registry.put(noActivity)

	// Session B: active just now — within threshold, must skip.
	recent := newConnectionSession("k-recent", 2, time.Unix(0, 0))
	recent.markActivity()
	recent.state.Load().recordDownload(opGet, 10, 0)
	registry.put(recent)

	// Session C: last activity was long ago AND op recorded — must flush.
	idleWithOps := newConnectionSession("k-idle-ops", 3, time.Unix(0, 0))
	idleWithOps.state.Load().recordDownload(opGet, 20, 0)
	// Manually backdate lastActivityAt to simulate elapsed idle time.
	idleWithOps.activityMu.Lock()
	idleWithOps.lastActivityAt = time.Now().Add(-1 * time.Hour)
	idleWithOps.activityMu.Unlock()
	registry.put(idleWithOps)

	// Session D: idle but no ops recorded (markActivity flipped the flag but
	// the RPC errored before recording). Must skip — nothing to serialise.
	idleNoOps := newConnectionSession("k-idle-noops", 4, time.Unix(0, 0))
	idleNoOps.markActivity()
	idleNoOps.activityMu.Lock()
	idleNoOps.lastActivityAt = time.Now().Add(-1 * time.Hour)
	idleNoOps.activityMu.Unlock()
	registry.put(idleNoOps)

	h.flushIdle(time.Now(), 15*time.Second)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "only the idle-with-ops session writes an intermediate sidecar")

	// Counters on the idle-with-ops session must have reset — swap installed a fresh slice.
	assert.True(t, idleWithOps.state.Load().getStats().blobStats.IsEmpty(), "post-flush state must be fresh")
}

// TestFlushIdle_IntermediateSidecarMarkedNonTerminal pins the Terminal flag:
// idle flushes write terminal=false so future consumers can split per-build
// vs full-session accounting.
func TestFlushIdle_IntermediateSidecarMarkedNonTerminal(t *testing.T) {
	dir := t.TempDir()
	registry := newSessionRegistry()
	h := &sidecarStatsHandler{
		writer:   newSidecarWriter(dir, nil),
		registry: registry,
	}

	cs := newConnectionSession("k", 42, time.Unix(0, 0))
	cs.state.Load().recordDownload(opGet, 10, 0)
	cs.activityMu.Lock()
	cs.lastActivityAt = time.Now().Add(-1 * time.Hour)
	cs.activityMu.Unlock()
	registry.put(cs)

	h.flushIdle(time.Now(), 15*time.Second)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	body, err := os.ReadFile(dir + "/" + entries[0].Name())
	require.NoError(t, err)
	assert.Contains(t, string(body), `"terminal":false`, "intermediate flush writes terminal=false")
}
