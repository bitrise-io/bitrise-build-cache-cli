package proxy

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	grpcstats "google.golang.org/grpc/stats"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildidentity"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/deriveddata"
)

// perConnIdleThreshold is the per-connection inactivity window after which an
// intermediate sidecar is flushed. The wrapper's own inactivity timer sits on
// top of SetSession; this one runs independently and covers the IDE plugin
// case where ConnEnd only fires when Xcode fully quits. Under the manifest-
// anchored InvocationID scheme the sidecar no longer dictates identity, so
// the 15s window is enough to keep intermediate flushes rare on a quiet IDE.
const perConnIdleThreshold = 15 * time.Second

// derivedIDFlushMultiplier scales perConnIdleThreshold for the empty-stat flush path so quiet derived-ID connections surface on disk.
const derivedIDFlushMultiplier = 3

// connSessionCtxKey tags the per-connection session on the gRPC-scoped ctx.
// Per-conn accounting runs additively alongside the global p.sessionState,
// which remains the source of truth for the SetSession / GetSessionStats
// wrapper flow — do not change behaviour there.
type connSessionCtxKey struct{}

// connectionSession is the per-conn counterpart of Proxy.sessionState. One
// per accepted connection; flushed terminally on gRPC ConnEnd and
// intermediately on per-conn inactivity.
type connectionSession struct {
	// key is the synthetic RemoteAddr tag used across registry and listener.
	key string

	peerPID    int
	acceptedAt time.Time

	// state is swapped under swapState() on intermediate flushes so a
	// just-captured slice can be written while an in-flight RPC continues
	// recording into the new one. In-flight RPCs land fully in old or fully
	// in new, never split.
	state atomic.Pointer[sessionState]

	// derivedInvocationID is set by TagConn when the peer's ancestry anchors to
	// an xcodebuild/Xcode/SWBBuildService ancestor and no wrapper session is
	// active. Empty when the wrapper owns the session or no anchor was found.
	// For IDE peers it is re-derived every tick ResolveAndMaybeReseat sees a
	// new <hex>.xcbuilddata edge, so each ⌘B lands on its own BE row.
	derivedInvocationID string

	// buildRequestHex is the last-observed <32-hex>.xcbuilddata stem for an
	// IDE peer. Empty on CLI / wrapper peers (reseat skips them) and until the
	// first poll tick that locates a build-request dir. Compared under
	// activityMu to the newly-polled hex to decide whether to reseat.
	buildRequestHex string

	// derivedDataPath is the -derivedDataPath value parsed off the first
	// "xcodebuild" ancestor's cmdline, resolved against that process's cwd.
	// Empty when the flag was not supplied, the ancestor could not be
	// introspected, or the peer has no xcodebuild ancestor at all. Shipped on
	// the sidecar so the enrichment watcher can glob project-local DD roots.
	derivedDataPath string

	// ancestryEntries caches the resolved rich chain for the peer; sidecar
	// write time projects names from this instead of re-walking (the PID may be
	// dead by then).
	ancestryEntries []AncestryEntry

	// terminalOnce guards the terminal flush against a GracefulStop flushAll
	// racing ConnEnd. Intermediate flushes have no sync.Once — they can fire
	// repeatedly across the connection's lifetime.
	terminalOnce sync.Once

	// anchoredEmptyOnce guards the terminal marker written by flushIdle when a
	// connection anchored a derivedInvocationID but then stayed idle past
	// derivedIDFlushMultiplier * threshold with no activity to flush. The gate
	// is sticky (anchoredAndAged stays true on every subsequent idle tick), so
	// without the Once we would emit an empty sidecar per tick until ConnEnd.
	anchoredEmptyOnce sync.Once

	// Zero lastActivityAt means no cache RPC ran (health probe, aborted
	// dial); flushTerminal suppresses the sidecar write in that case. Also
	// drives the idle check for intermediate flushes.
	activityMu     sync.Mutex
	lastActivityAt time.Time

	// buildStartedAt is stamped when the connection first anchors a
	// derivedInvocationID (TagConn) and re-stamped on every ⌘B edge
	// (ResolveAndMaybeReseat). Enriches the pseudo-group Start so a long-lived
	// SWBBuildService daemon's AcceptedAt doesn't inflate per-build duration to
	// the daemon's uptime. Guarded by activityMu.
	buildStartedAt time.Time

	// buildRequest carries the Command/Action/Configuration/Container the
	// resolver parsed from `<hex>.xcbuilddata/build-request.json` on the last
	// reseat tick. Lets the wrapperless pseudo-group emit a Command token
	// (`indexbuild`, `prepareForIndexing`, etc.) instead of a bare `build`.
	// Guarded by activityMu; snapshot BEFORE swap on reseat so the flushed
	// sidecar carries the pre-reseat classification.
	buildRequest deriveddata.BuildRequest
}

func (c *connectionSession) markActivity() {
	c.activityMu.Lock()
	defer c.activityMu.Unlock()

	c.lastActivityAt = time.Now()
}

// invocationID returns the current derivedInvocationID under the activity mutex
// so a concurrent ResolveAndMaybeReseat cannot tear the read.
func (c *connectionSession) invocationID() string {
	c.activityMu.Lock()
	defer c.activityMu.Unlock()

	return c.derivedInvocationID
}

// setInvocationID stores the derived InvocationID under the activity mutex.
// Called at TagConn time (once) and by ResolveAndMaybeReseat on every ⌘B edge.
func (c *connectionSession) setInvocationID(id string) {
	c.activityMu.Lock()
	defer c.activityMu.Unlock()

	c.derivedInvocationID = id
}

// swapBuildRequestHex atomically compares-and-updates buildRequestHex,
// returning true when the hex changed (fresh edge → caller reseats).
func (c *connectionSession) swapBuildRequestHex(next string) bool {
	c.activityMu.Lock()
	defer c.activityMu.Unlock()

	if c.buildRequestHex == next {
		return false
	}

	c.buildRequestHex = next

	return true
}

// buildRequestHexSnapshot returns the current buildRequestHex under the
// activity mutex so a concurrent ResolveAndMaybeReseat cannot tear the read.
func (c *connectionSession) buildRequestHexSnapshot() string {
	c.activityMu.Lock()
	defer c.activityMu.Unlock()

	return c.buildRequestHex
}

// buildStartedAtSnapshot returns the current buildStartedAt under the activity
// mutex so a concurrent reseat cannot tear the read.
func (c *connectionSession) buildStartedAtSnapshot() time.Time {
	c.activityMu.Lock()
	defer c.activityMu.Unlock()

	return c.buildStartedAt
}

// setBuildStartedAt stores the per-build start timestamp under the activity
// mutex. Called at TagConn time when the first derivedInvocationID is anchored
// and re-stamped by ResolveAndMaybeReseat on every ⌘B edge.
func (c *connectionSession) setBuildStartedAt(t time.Time) {
	c.activityMu.Lock()
	defer c.activityMu.Unlock()

	c.buildStartedAt = t
}

// buildRequestSnapshot returns the current parsed build-request classification
// under the activity mutex.
func (c *connectionSession) buildRequestSnapshot() deriveddata.BuildRequest {
	c.activityMu.Lock()
	defer c.activityMu.Unlock()

	return c.buildRequest
}

// setBuildRequest stores the parsed build-request classification under the
// activity mutex. Called by ResolveAndMaybeReseat after a successful hex swap
// so the subsequent sidecar flushes carry the current ⌘B's classification.
func (c *connectionSession) setBuildRequest(br deriveddata.BuildRequest) {
	c.activityMu.Lock()
	defer c.activityMu.Unlock()

	c.buildRequest = br
}

func (c *connectionSession) hadActivity() bool {
	c.activityMu.Lock()
	defer c.activityMu.Unlock()

	return !c.lastActivityAt.IsZero()
}

// hadActivityWithin reports whether the last recorded RPC falls inside the
// threshold measured from now. A zero lastActivityAt (no RPCs yet) counts as
// "not within".
func (c *connectionSession) hadActivityWithin(threshold time.Duration, now time.Time) bool {
	c.activityMu.Lock()
	defer c.activityMu.Unlock()

	if c.lastActivityAt.IsZero() {
		return false
	}

	return now.Sub(c.lastActivityAt) < threshold
}

// swapState atomically installs a fresh sessionState and returns the previous
// one. Any RPC that already read the old pointer finishes recording into it;
// any RPC that reads after the swap records into the new one.
func (c *connectionSession) swapState() *sessionState {
	fresh := newSessionState()

	return c.state.Swap(fresh)
}

func newConnectionSession(key string, peerPID int, acceptedAt time.Time) *connectionSession {
	cs := &connectionSession{ //nolint:exhaustruct // optional fields populated by TagConn
		key:        key,
		peerPID:    peerPID,
		acceptedAt: acceptedAt,
	}
	cs.state.Store(newSessionState())

	return cs
}

// sessionFromContext returns the per-conn session hung off the gRPC call ctx,
// or nil if the stats handler never saw a ConnBegin (bufconn tests).
func sessionFromContext(ctx context.Context) *connectionSession {
	v, _ := ctx.Value(connSessionCtxKey{}).(*connectionSession)

	return v
}

func contextWithSession(ctx context.Context, cs *connectionSession) context.Context {
	ctx = context.WithValue(ctx, connSessionCtxKey{}, cs)

	return kv.WithInvocationID(ctx, cs.derivedInvocationID)
}

// rewrapInvocationID re-stamps the KV InvocationID on ctx from the per-conn
// session's current derivedInvocationID. Used by the unary interceptor so a
// mid-connection reseat (ResolveAndMaybeReseat) reaches the KV layer on the
// next RPC without re-running TagConn. ctx without a per-conn session passes
// through unchanged.
func rewrapInvocationID(ctx context.Context) context.Context {
	cs := sessionFromContext(ctx)
	if cs == nil {
		return ctx
	}

	id := cs.invocationID()
	if id == "" {
		return ctx
	}

	return kv.WithInvocationID(ctx, id)
}

// recorder mirrors every record call to the global sessionState and, when
// set, to the per-conn one. saveKeyOnce reports the global's decision so
// long-lived IDE connections and wrapper sessions can't disagree on what's
// already pushed.
type recorder struct {
	global *sessionState
	conn   *sessionState
}

func (r *recorder) recordDownload(op cacheOp, bytes int64, duration time.Duration) {
	r.global.recordDownload(op, bytes, duration)
	if r.conn != nil {
		r.conn.recordDownload(op, bytes, duration)
	}
}

func (r *recorder) recordUpload(op cacheOp, bytes int64, duration time.Duration) {
	r.global.recordUpload(op, bytes, duration)
	if r.conn != nil {
		r.conn.recordUpload(op, bytes, duration)
	}
}

func (r *recorder) recordMiss(op cacheOp) {
	r.global.recordMiss(op)
	if r.conn != nil {
		r.conn.recordMiss(op)
	}
}

func (r *recorder) recordError(op cacheOp, err error) {
	r.global.recordError(op, err)
	if r.conn != nil {
		r.conn.recordError(op, err)
	}
}

func (r *recorder) saveKeyOnce(key string) bool {
	loaded := r.global.saveKeyOnce(key)
	if r.conn != nil {
		r.conn.saveKeyOnce(key)
	}

	return loaded
}

func (r *recorder) markKeyUnsaved(key string) {
	r.global.markKeyUnsaved(key)
	if r.conn != nil {
		r.conn.markKeyUnsaved(key)
	}
}

func (r *recorder) recordSkippedAlreadySaved(op cacheOp) {
	r.global.recordSkippedAlreadySaved(op)
	if r.conn != nil {
		r.conn.recordSkippedAlreadySaved(op)
	}
}

// sidecarStatsHandler is the glue between gRPC's connection lifecycle and the
// per-conn session + sidecar writer. TagConn + HandleConn carry the ConnBegin
// / ConnEnd signals.
type sidecarStatsHandler struct {
	listener *PeerListener
	registry *sessionRegistry
	writer   *sidecarWriter

	// peerAncestryEntries resolves the PEER PID's ancestry as rich entries.
	// INVARIANT: this helper is only ever called with a PID that originates
	// from PeerListener.LookupConn — never from the proxy's own PID chain, or
	// derivation would overcount every connection as the proxy's parent
	// xcodebuild/Xcode. Covered by TestDerivePeerInvocationID_NeverUsesProxyAncestry.
	peerAncestryEntries func(pid int) []AncestryEntry

	// derivedDataPathFromEntries parses -derivedDataPath off the first
	// xcodebuild entry in the cached chain. Empty when nothing to anchor on or
	// the peer's cmdline is unreadable. Injectable so tests can skip
	// gopsutil-driven process introspection.
	derivedDataPathFromEntries func(entries []AncestryEntry) string

	// wrapperSessionActive returns true when p.currentSession != nil at
	// TagConn time; derivation skips in that case because the wrapper already
	// owns the InvocationID.
	wrapperSessionActive func() bool

	// buildRequestResolver returns the newest build-request hash scoped to a
	// peer's DD root. nil disables the per-⌘B reseat path; CLI/wrapper peers
	// also skip even when it is set (see ResolveAndMaybeReseat for the gate).
	// Injectable so the tick cadence and plist/glob I/O can be tested
	// synchronously without a filesystem.
	buildRequestResolver BuildRequestResolver
}

// BuildRequestResolver returns the newest <32-hex>.xcbuilddata stem scoped to
// a peer's DerivedData plus the parsed build-request classification. Returns
// (zero, false) when no entry is found. The resolver takes the per-conn session
// so an implementation can read the cached derivedDataPath / ancestry without
// extra wiring.
type BuildRequestResolver func(cs *connectionSession) (BuildRequestResult, bool)

// BuildRequestResult bundles the hex stem the resolver matched and the
// best-effort classification parsed from that entry's build-request.json. The
// Request field is the zero value when the file is absent or malformed —
// callers must tolerate an empty Request alongside a populated Hex.
type BuildRequestResult struct {
	Hex     string
	Request deriveddata.BuildRequest
}

var _ grpcstats.Handler = (*sidecarStatsHandler)(nil)

// TagRPC is a no-op; no RPC-scoped state to attach.
func (h *sidecarStatsHandler) TagRPC(ctx context.Context, _ *grpcstats.RPCTagInfo) context.Context {
	return ctx
}

func (h *sidecarStatsHandler) HandleRPC(_ context.Context, _ grpcstats.RPCStats) {}

// TagConn mints a connectionSession keyed on the peer-listener's lookup and
// hangs it off the ctx. If the listener never saw this conn (not routed
// through NewPeerListener), returns the ctx unchanged.
func (h *sidecarStatsHandler) TagConn(ctx context.Context, info *grpcstats.ConnTagInfo) context.Context {
	if h.listener == nil || info == nil || info.RemoteAddr == nil {
		return ctx
	}

	key := info.RemoteAddr.String()
	pc, ok := h.listener.LookupConn(key)
	if !ok {
		return ctx
	}

	cs := newConnectionSession(key, pc.peerPID, pc.acceptedAt)
	cs.ancestryEntries = h.resolveEntries(pc.peerPID)
	cs.derivedInvocationID = h.derivePeerInvocationID(cs.ancestryEntries, "")
	cs.derivedDataPath = h.resolveDerivedDataPath(cs.ancestryEntries)
	// Skip stamping when derivation produced no ID — health probes and
	// wrapper-owned conns have no row to attribute, and a zero start keeps
	// OrphanSidecarsGroupedByInvocation falling back to AcceptedAt.
	if cs.derivedInvocationID != "" {
		cs.setBuildStartedAt(time.Now())
	}
	h.registry.put(cs)

	return contextWithSession(ctx, cs)
}

// resolveEntries returns the cached ancestry entries for peerPID, or nil when
// the resolver is not wired (bufconn tests). Callers project / parse off the
// cached slice to avoid a second walk when the PEER PID may be dead already.
func (h *sidecarStatsHandler) resolveEntries(peerPID int) []AncestryEntry {
	if h.peerAncestryEntries == nil {
		return nil
	}

	return h.peerAncestryEntries(peerPID)
}

// resolveDerivedDataPath parses -derivedDataPath from the first xcodebuild
// entry's cmdline. Returns "" when the resolver is not wired or no xcodebuild
// anchor is present (health probe / unrelated peer).
func (h *sidecarStatsHandler) resolveDerivedDataPath(entries []AncestryEntry) string {
	if h.derivedDataPathFromEntries == nil || len(entries) == 0 {
		return ""
	}

	return h.derivedDataPathFromEntries(entries)
}

// derivePeerInvocationID returns a deterministic InvocationID derived from
// the peer's first anchorable ancestor, optionally salted by a build-request
// hex. Returns "" when:
//   - the wrapper owns the current session (its ID wins),
//   - the peer ancestry lacks an anchorable name (health probe / unrelated),
//   - the resolver was not wired (bufconn tests).
//
// Empty buildRequestHex yields byte-identical output to the pre-reseat
// Derive — CLI/wrapper paths keep their old IDs.
func (h *sidecarStatsHandler) derivePeerInvocationID(entries []AncestryEntry, buildRequestHex string) string {
	if h.wrapperSessionActive != nil && h.wrapperSessionActive() {
		return ""
	}
	if len(entries) == 0 {
		return ""
	}

	anchor, ok := buildidentity.AnchorFromAncestry(entries)
	if !ok {
		return ""
	}

	anchor.BuildRequestHex = buildRequestHex

	return buildidentity.Derive(anchor)
}

// isIDEAncestry reports whether the peer's ancestry roots in Xcode.app rather
// than an xcodebuild CLI run. Xcode's own ancestry contains "Xcode" and/or
// "SWBBuildService" but NOT "xcodebuild"; a CLI run contains "xcodebuild"
// (optionally wrapped by shells/launchers). The per-⌘B reseat path only
// applies to IDE peers — CLI peers have a stable InvocationID per process
// and reseating them would split their rows apart for no gain.
func isIDEAncestry(entries []AncestryEntry) bool {
	hasIDE := false
	for _, e := range entries {
		if e.Name == "xcodebuild" {
			return false
		}
		if e.Name == "Xcode" || e.Name == "SWBBuildService" {
			hasIDE = true
		}
	}

	return hasIDE
}

// ResolveAndMaybeReseat is called by the per-conn flush timer (same cadence,
// coalesced into one tick). For an IDE peer with a changed build-request hex
// it re-derives the InvocationID so subsequent RPCs land on a fresh BE row;
// CLI/wrapper peers and peers lacking the resolver pass through unchanged.
//
// Returns true when the ID was reseated. Exposed on the handler so a test can
// drive one tick without spinning the timer.
func (h *sidecarStatsHandler) ResolveAndMaybeReseat(cs *connectionSession) bool {
	if h == nil || h.buildRequestResolver == nil || cs == nil {
		return false
	}
	if h.wrapperSessionActive != nil && h.wrapperSessionActive() {
		return false
	}
	if !isIDEAncestry(cs.ancestryEntries) {
		return false
	}

	resolved, ok := h.buildRequestResolver(cs)
	if !ok || resolved.Hex == "" {
		return false
	}

	// Snapshot the OLD identity BEFORE swapBuildRequestHex mutates state so the
	// flushed sidecar carries the hex that generated its blob stats — the
	// enricher keys on it to bridge pseudo-group PUTs back to the manifest row.
	oldID := cs.invocationID()
	oldHex := cs.buildRequestHexSnapshot()
	oldBuildStartedAt := cs.buildStartedAtSnapshot()
	oldBuildRequest := cs.buildRequestSnapshot()

	if !cs.swapBuildRequestHex(resolved.Hex) {
		return false
	}

	// Flush whatever stats accumulated under the old ID BEFORE the swap, so the
	// pre-reseat ⌘B's blobs land on its own BE row rather than folding into the
	// freshly-derived ID the next RPC will use.
	if h.writer != nil {
		old := cs.swapState()
		if !old.getStats().blobStats.IsEmpty() {
			sidecar := newSessionSidecar(cs, old, time.Now(), false, sidecarIdentity{InvocationID: oldID, BuildRequestHex: oldHex, BuildStartedAt: oldBuildStartedAt, BuildRequest: oldBuildRequest})
			h.writer.write(sidecar)
		}
	}

	cs.setBuildStartedAt(time.Now())
	cs.setBuildRequest(resolved.Request)
	cs.setInvocationID(h.derivePeerInvocationID(cs.ancestryEntries, resolved.Hex))

	return true
}

// HandleConn flushes the per-conn sidecar on *grpcstats.ConnEnd. ConnBegin is
// handled by TagConn.
func (h *sidecarStatsHandler) HandleConn(ctx context.Context, cs grpcstats.ConnStats) {
	if _, ok := cs.(*grpcstats.ConnEnd); !ok {
		return
	}

	session := sessionFromContext(ctx)
	if session == nil {
		return
	}

	h.flushTerminal(session)
	h.cleanupForContext(ctx)
}

// flushTerminal writes the final sidecar for a connection (ConnEnd /
// GracefulStop). sync.Once dedups across the two paths. A conn that never
// issued an RPC (health probe, aborted dial) emits nothing — no file churn for
// the reader.
func (h *sidecarStatsHandler) flushTerminal(cs *connectionSession) {
	cs.terminalOnce.Do(func() {
		if !cs.hadActivity() {
			return
		}

		sidecar := newSessionSidecar(cs, cs.state.Load(), time.Now(), true, sidecarIdentity{InvocationID: cs.invocationID(), BuildRequestHex: cs.buildRequestHexSnapshot(), BuildStartedAt: cs.buildStartedAtSnapshot(), BuildRequest: cs.buildRequestSnapshot()})
		h.writer.write(sidecar)
	})
}

// flushIdle writes an intermediate sidecar for every outstanding session that
// has activity but has gone quiet for at least threshold. The state pointer is
// swapped atomically; in-flight RPCs land fully in old or fully in new, never
// split. Called by the per-conn flush timer, so a long-lived IDE peer's
// cacheBlobStats land on BE mid-day instead of waiting for Xcode to quit.
func (h *sidecarStatsHandler) flushIdle(now time.Time, threshold time.Duration) {
	h.registry.mu.Lock()
	sessions := make([]*connectionSession, 0, len(h.registry.byKey))
	for _, cs := range h.registry.byKey {
		sessions = append(sessions, cs)
	}
	h.registry.mu.Unlock()

	for _, cs := range sessions {
		if !cs.hadActivity() {
			continue
		}

		if cs.hadActivityWithin(threshold, now) {
			continue
		}

		hasActivity := !cs.state.Load().getStats().blobStats.IsEmpty()
		anchoredAndAged := cs.derivedInvocationID != "" &&
			!cs.hadActivityWithin(threshold*derivedIDFlushMultiplier, now)
		if !hasActivity && !anchoredAndAged {
			continue
		}

		if !hasActivity {
			cs.anchoredEmptyOnce.Do(func() {
				old := cs.swapState()
				sidecar := newSessionSidecar(cs, old, now, false, sidecarIdentity{InvocationID: cs.invocationID(), BuildRequestHex: cs.buildRequestHexSnapshot(), BuildStartedAt: cs.buildStartedAtSnapshot(), BuildRequest: cs.buildRequestSnapshot()})
				h.writer.write(sidecar)
			})

			continue
		}

		old := cs.swapState()
		sidecar := newSessionSidecar(cs, old, now, false, sidecarIdentity{InvocationID: cs.invocationID(), BuildRequestHex: cs.buildRequestHexSnapshot(), BuildStartedAt: cs.buildStartedAtSnapshot(), BuildRequest: cs.buildRequestSnapshot()})
		h.writer.write(sidecar)
	}
}

// cleanupForContext removes the registry + listener entries for the ctx's
// session. No-op if the stats handler never tagged this ctx.
func (h *sidecarStatsHandler) cleanupForContext(ctx context.Context) {
	session := sessionFromContext(ctx)
	if session == nil {
		return
	}

	h.registry.forget(session.key)

	if h.listener != nil {
		h.listener.Forget(session.key)
	}
}

// sessionRegistry is the key → *connectionSession map, keyed by the same
// synthetic RemoteAddr tag the peer-listener uses.
type sessionRegistry struct {
	mu    sync.Mutex
	byKey map[string]*connectionSession
}

func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{
		byKey: make(map[string]*connectionSession),
	}
}

func (r *sessionRegistry) put(cs *connectionSession) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.byKey[cs.key] = cs
}

func (r *sessionRegistry) forget(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.byKey, key)
}

// flushAll fires every outstanding session's sidecar. Called on Proxy
// shutdown so a graceful stop doesn't lose in-flight sessions.
func (r *sessionRegistry) flushAll(flushFn func(*connectionSession)) {
	r.mu.Lock()
	sessions := make([]*connectionSession, 0, len(r.byKey))
	for _, cs := range r.byKey {
		sessions = append(sessions, cs)
	}
	r.mu.Unlock()

	for _, cs := range sessions {
		flushFn(cs)
	}
}
