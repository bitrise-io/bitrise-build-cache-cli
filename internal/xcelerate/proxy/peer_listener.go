package proxy

import (
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// PeerListener wraps a net.Listener so every accepted connection carries the
// peer process's PID (darwin only — zero elsewhere). The sidecar writer uses
// the PID to correlate a session with the compiler that opened it.
type PeerListener struct {
	net.Listener

	nextConnID atomic.Int64

	mu    sync.Mutex
	conns map[string]*peerConn // keyed by RemoteAddr().String()
}

// NewPeerListener returns a listener that tags each accepted connection with
// the peer PID (darwin) and a unique synthetic remote-addr string the gRPC
// stats handler can look up via LookupConn.
func NewPeerListener(inner net.Listener) *PeerListener {
	return &PeerListener{
		Listener: inner,
		conns:    make(map[string]*peerConn),
	}
}

// Accept returns a *peerConn whose RemoteAddr() carries a connection-unique
// tag. The underlying conn is kept embedded so gRPC's transport sees the real
// read/write / close semantics unchanged.
func (l *PeerListener) Accept() (net.Conn, error) {
	raw, err := l.Listener.Accept()
	if err != nil {
		//nolint:wrapcheck // passthrough from the embedded listener
		return nil, err
	}

	id := l.nextConnID.Add(1)
	tag := "bitrise-peer-conn-" + strconv.FormatInt(id, 10)

	pid, _ := extractPeerPID(raw)

	pc := &peerConn{
		Conn:       raw,
		remote:     taggedAddr{inner: raw.RemoteAddr(), tag: tag},
		peerPID:    pid,
		acceptedAt: time.Now(),
	}

	l.mu.Lock()
	l.conns[tag] = pc
	l.mu.Unlock()

	return pc, nil
}

// LookupConn returns the *peerConn that was tagged with the given RemoteAddr
// string. gRPC's stats.Handler.TagConn uses this to pull the peer PID.
func (l *PeerListener) LookupConn(remoteAddrStr string) (*peerConn, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	pc, ok := l.conns[remoteAddrStr]

	return pc, ok
}

// Forget drops the registry entry. Call on connection close to bound memory.
func (l *PeerListener) Forget(remoteAddrStr string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.conns, remoteAddrStr)
}

// peerConn wraps a raw net.Conn with the peer PID captured at Accept time
// plus a synthetic RemoteAddr that gRPC's stats.Handler can use as a lookup
// key (AF_UNIX anonymous clients have an empty RemoteAddr otherwise).
type peerConn struct {
	net.Conn

	remote     taggedAddr
	peerPID    int
	acceptedAt time.Time
}

func (p *peerConn) RemoteAddr() net.Addr { return p.remote }

// taggedAddr returns tag as String() so gRPC transport logs stay informative
// (socket paths otherwise collide) while Network() stays correct.
type taggedAddr struct {
	inner net.Addr
	tag   string
}

func (t taggedAddr) Network() string {
	if t.inner == nil {
		return "unix"
	}

	return t.inner.Network()
}

func (t taggedAddr) String() string { return t.tag }
