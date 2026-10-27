package proxy

import (
	"context"
	"sync"
	"time"

	grpcstats "google.golang.org/grpc/stats"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildidentity"
)

// connSessionCtxKey tags the per-connection session on the gRPC-scoped ctx.
// Per-conn accounting runs additively alongside the global p.sessionState,
// which remains the source of truth for the SetSession / GetSessionStats
// wrapper flow — do not change behaviour there.
type connSessionCtxKey struct{}

// connectionSession is the per-conn counterpart of Proxy.sessionState. One
// per accepted connection; flushed exactly once on gRPC ConnEnd.
type connectionSession struct {
	// key is the synthetic RemoteAddr tag used across registry and listener.
	key string

	peerPID    int
	acceptedAt time.Time
	state      *sessionState

	// derivedInvocationID is set by TagConn when the peer's ancestry anchors to
	// an xcodebuild/Xcode/SWBBuildService ancestor and no wrapper session is
	// active. Empty when the wrapper owns the session or no anchor was found.
	derivedInvocationID string

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

	// flushOnce guards against a GracefulStop flushAll racing ConnEnd.
	flushOnce sync.Once

	// Zero firstActivityAt means no cache RPC ran (health probe, aborted
	// dial); flushSession suppresses the sidecar write in that case.
	activityMu      sync.Mutex
	firstActivityAt time.Time
}

func (c *connectionSession) markActivity() {
	c.activityMu.Lock()
	defer c.activityMu.Unlock()

	if c.firstActivityAt.IsZero() {
		c.firstActivityAt = time.Now()
	}
}

func (c *connectionSession) hadActivity() bool {
	c.activityMu.Lock()
	defer c.activityMu.Unlock()

	return !c.firstActivityAt.IsZero()
}

func newConnectionSession(key string, peerPID int, acceptedAt time.Time) *connectionSession {
	return &connectionSession{
		key:        key,
		peerPID:    peerPID,
		acceptedAt: acceptedAt,
		state:      newSessionState(),
	}
}

// sessionFromContext returns the per-conn session hung off the gRPC call ctx,
// or nil if the stats handler never saw a ConnBegin (bufconn tests).
func sessionFromContext(ctx context.Context) *connectionSession {
	v, _ := ctx.Value(connSessionCtxKey{}).(*connectionSession)

	return v
}

func contextWithSession(ctx context.Context, cs *connectionSession) context.Context {
	return context.WithValue(ctx, connSessionCtxKey{}, cs)
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
	cs.derivedInvocationID = h.derivePeerInvocationID(cs.ancestryEntries)
	cs.derivedDataPath = h.resolveDerivedDataPath(cs.ancestryEntries)
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
// the peer's first anchorable ancestor. Returns "" when:
//   - the wrapper owns the current session (its ID wins),
//   - the peer ancestry lacks an anchorable name (health probe / unrelated),
//   - the resolver was not wired (bufconn tests).
func (h *sidecarStatsHandler) derivePeerInvocationID(entries []AncestryEntry) string {
	if h.wrapperSessionActive != nil && h.wrapperSessionActive() {
		return ""
	}
	if len(entries) == 0 {
		return ""
	}

	bidEntries := make([]buildidentity.AncestryEntry, 0, len(entries))
	for _, e := range entries {
		bidEntries = append(bidEntries, buildidentity.AncestryEntry{PID: e.PID, Name: e.Name, StartTimeMS: e.StartTimeMS})
	}

	anchor, ok := buildidentity.AnchorFromAncestry(bidEntries)
	if !ok {
		return ""
	}

	return buildidentity.Derive(anchor)
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

	h.flushSession(session)
	h.cleanupForContext(ctx)
}

// flushSession writes the sidecar exactly once (sync.Once dedup across the
// ConnEnd and GracefulStop flushAll paths). A conn that never issued an RPC
// (health probe, aborted dial) emits nothing — no file churn for the reader.
func (h *sidecarStatsHandler) flushSession(cs *connectionSession) {
	cs.flushOnce.Do(func() {
		if !cs.hadActivity() {
			return
		}

		sidecar := newSessionSidecar(cs, time.Now())
		h.writer.write(sidecar)
	})
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
