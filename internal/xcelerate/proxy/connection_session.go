package proxy

import (
	"context"
	"sync"
	"time"

	grpcstats "google.golang.org/grpc/stats"
)

// connSessionCtxKey tags the per-connection session on the gRPC-scoped ctx
// so UnaryInterceptor can additively write to it alongside the global
// p.sessionState — the global stays the source of truth for the SetSession
// / GetSessionStats wrapper flow and must not change behaviour.
type connSessionCtxKey struct{}

// connectionSession is the per-conn counterpart of Proxy.sessionState. One
// per accepted connection; flushed exactly once on the first of (gRPC
// ConnEnd, per-conn inactivity timeout).
type connectionSession struct {
	peerPID    int
	acceptedAt time.Time
	state      *sessionState

	// flushOnce guards the sidecar write so a late ConnEnd after an inactivity
	// fire (or vice versa) is a no-op.
	flushOnce sync.Once

	// touchMu guards lastActivity + inactivityTimer the same way Proxy does.
	touchMu         sync.Mutex
	lastActivity    time.Time
	inactivityTimer *time.Timer
}

func newConnectionSession(peerPID int, acceptedAt time.Time) *connectionSession {
	return &connectionSession{
		peerPID:    peerPID,
		acceptedAt: acceptedAt,
		state:      newSessionState(),
	}
}

// touch records activity for the per-conn inactivity timer, arming it on the
// first call. onInactivity fires flushFn when the window elapses with no new
// touches.
func (c *connectionSession) touch(window time.Duration, flushFn func()) {
	c.touchMu.Lock()
	defer c.touchMu.Unlock()

	c.lastActivity = time.Now()

	if c.inactivityTimer == nil && window > 0 {
		c.inactivityTimer = time.AfterFunc(window, func() {
			c.onInactivity(window, flushFn)
		})
	}
}

func (c *connectionSession) onInactivity(window time.Duration, flushFn func()) {
	c.touchMu.Lock()

	elapsed := time.Since(c.lastActivity)
	if remaining := window - elapsed; remaining > 0 {
		c.inactivityTimer = time.AfterFunc(remaining, func() {
			c.onInactivity(window, flushFn)
		})
		c.touchMu.Unlock()

		return
	}

	c.inactivityTimer = nil
	c.touchMu.Unlock()

	flushFn()
}

func (c *connectionSession) stopTimer() {
	c.touchMu.Lock()
	defer c.touchMu.Unlock()

	if c.inactivityTimer != nil {
		c.inactivityTimer.Stop()
		c.inactivityTimer = nil
	}
}

// sessionFromContext returns the per-conn session hung off the gRPC call ctx.
// Nil means the stats handler never saw a ConnBegin (bufconn tests, non-gRPC
// callers) — the global sessionState path still runs so no counters are lost.
func sessionFromContext(ctx context.Context) *connectionSession {
	v, _ := ctx.Value(connSessionCtxKey{}).(*connectionSession)

	return v
}

// contextWithSession stamps the session on the ctx returned by TagConn. gRPC
// derives every subsequent RPC ctx on this connection from it.
func contextWithSession(ctx context.Context, cs *connectionSession) context.Context {
	return context.WithValue(ctx, connSessionCtxKey{}, cs)
}

// fanoutState writes every sessionState mutation to each wrapped state. Used by
// the Proxy handlers so counters land in both the global sessionState (keeps
// SetSession/GetSessionStats behaviour untouched) and the per-conn sessionState
// (feeds the sidecar on connection close).
type fanoutState struct {
	states []*sessionState
}

func (f *fanoutState) recordDownload(op cacheOp, bytes int64, duration time.Duration) {
	for _, s := range f.states {
		s.recordDownload(op, bytes, duration)
	}
}

func (f *fanoutState) recordUpload(op cacheOp, bytes int64, duration time.Duration) {
	for _, s := range f.states {
		s.recordUpload(op, bytes, duration)
	}
}

func (f *fanoutState) recordMiss(op cacheOp) {
	for _, s := range f.states {
		s.recordMiss(op)
	}
}

func (f *fanoutState) recordError(op cacheOp, err error) {
	for _, s := range f.states {
		s.recordError(op, err)
	}
}

// saveKeyOnce fans the dedup check out but anchors the "already saved"
// decision on the global state so a long-lived IDE connection and a wrapper
// session agree on what has already been pushed.
func (f *fanoutState) saveKeyOnce(key string) bool {
	loaded := false

	for i, s := range f.states {
		v := s.saveKeyOnce(key)
		if i == 0 {
			loaded = v
		}
	}

	return loaded
}

func (f *fanoutState) markKeyUnsaved(key string) {
	for _, s := range f.states {
		s.markKeyUnsaved(key)
	}
}

func (f *fanoutState) recordSkippedAlreadySaved(op cacheOp) {
	for _, s := range f.states {
		s.recordSkippedAlreadySaved(op)
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
	// inactivityWindow is read lazily so callers that set Proxy.InactivityTimeout
	// after NewProxy still get the overridden window.
	inactivityWindow func() time.Duration
}

var _ grpcstats.Handler = (*sidecarStatsHandler)(nil)

// TagRPC is a no-op; UnaryInterceptor on the Proxy already enriches the ctx
// for method dispatch, and we have no RPC-scoped state to attach.
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

	pc, ok := h.listener.LookupConn(info.RemoteAddr.String())
	if !ok {
		return ctx
	}

	cs := newConnectionSession(pc.peerPID, pc.acceptedAt)
	h.registry.put(info.RemoteAddr.String(), cs)

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

// flushSession writes the sidecar exactly once (sync.Once dedup across
// ConnEnd / inactivity timer firing paths).
func (h *sidecarStatsHandler) flushSession(cs *connectionSession) {
	cs.flushOnce.Do(func() {
		cs.stopTimer()

		sidecar := newSessionSidecar(cs, time.Now(), h.ancestryResolver)
		h.writer.write(sidecar)
	})
}

// cleanupForContext removes the registry + listener entries. Walks the
// registry to find the key — HandleConn gives us the ctx but not the key.
func (h *sidecarStatsHandler) cleanupForContext(ctx context.Context) {
	session := sessionFromContext(ctx)
	if session == nil {
		return
	}

	key, ok := h.registry.keyFor(session)
	if !ok {
		return
	}

	h.registry.forget(key)

	if h.listener != nil {
		h.listener.Forget(key)
	}
}

// armInactivity exposes the per-conn touch path to the UnaryInterceptor —
// factored out so proxy.go stays focused on RPC dispatch.
func (h *sidecarStatsHandler) armInactivity(cs *connectionSession) {
	window := time.Duration(0)
	if h.inactivityWindow != nil {
		window = h.inactivityWindow()
	}

	cs.touch(window, func() {
		h.flushSession(cs)
	})
}

// sessionRegistry is the key → *connectionSession map, keyed by the same
// synthetic RemoteAddr tag the peer-listener uses.
type sessionRegistry struct {
	mu sync.Mutex
	// Two maps so cleanupForContext can look a session up by pointer without
	// scanning the whole registry on every HandleConn.
	byKey     map[string]*connectionSession
	keyBySess map[*connectionSession]string
}

func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{
		byKey:     make(map[string]*connectionSession),
		keyBySess: make(map[*connectionSession]string),
	}
}

func (r *sessionRegistry) put(key string, cs *connectionSession) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.byKey[key] = cs
	r.keyBySess[cs] = key
}

func (r *sessionRegistry) keyFor(cs *connectionSession) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	k, ok := r.keyBySess[cs]

	return k, ok
}

func (r *sessionRegistry) forget(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cs, ok := r.byKey[key]
	if !ok {
		return
	}

	delete(r.byKey, key)
	delete(r.keyBySess, cs)
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
