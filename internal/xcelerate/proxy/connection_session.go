package proxy

import (
	"context"
	"sync"
	"time"

	grpcstats "google.golang.org/grpc/stats"
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
	listener         *PeerListener
	registry         *sessionRegistry
	writer           *sidecarWriter
	ancestryResolver func(pid int) []string
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
	h.registry.put(cs)

	return contextWithSession(ctx, cs)
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

		sidecar := newSessionSidecar(cs, time.Now(), h.ancestryResolver)
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
