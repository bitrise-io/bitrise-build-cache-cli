package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
)

// SidecarSchemaVersion is the on-disk version written by the proxy and read by
// the enrichment side. Readers must leave files with a higher version on disk
// so a newer binary can pick them up — never delete, never attempt to parse.
const SidecarSchemaVersion = 2

// SessionSidecar is one line's worth of per-connection accounting written by
// the proxy on connection close (gRPC ConnEnd) or per-conn inactivity.
type SessionSidecar struct {
	SchemaVersion int    `json:"schema_version"`
	Source        string `json:"source"` // "proxy"
	SidecarUUID   string `json:"sidecar_uuid"`

	PeerPID      int       `json:"peer_pid"`
	PeerAncestry []string  `json:"peer_ancestry"`
	AcceptedAt   time.Time `json:"accepted_at"`
	ClosedAt     time.Time `json:"closed_at"`

	BlobStats *blobstats.Snapshot `json:"blobStats"`

	// WrapperSession is reserved for wrapper↔proxy invocation correlation; currently always nil.
	WrapperSession *WrapperSessionLink `json:"wrapper_session"`
}

// WrapperSessionLink is the reserved slot for a future wrapper-side
// correlation payload. Kept as a named type so the JSON shape is stable the
// day it stops being nil.
type WrapperSessionLink struct {
	InvocationID string `json:"invocation_id"`
}

type sidecarWriter struct {
	dir    string
	logger log.Logger
}

func newSidecarWriter(dir string, logger log.Logger) *sidecarWriter {
	return &sidecarWriter{dir: dir, logger: logger}
}

// write marshals the sidecar and renames into place atomically. Disk-full /
// perms errors log at Warn and drop silently — the sidecar stream is a
// correlation signal, not a persistence requirement.
func (w *sidecarWriter) write(s SessionSidecar) {
	if w == nil || w.dir == "" {
		return
	}

	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		w.warn("mkdir sessions dir: %s", err)

		return
	}

	body, err := json.Marshal(s)
	if err != nil {
		w.warn("marshal sidecar: %s", err)

		return
	}

	name := sidecarFilename(s)
	finalPath := filepath.Join(w.dir, name)
	tmpPath := finalPath + ".tmp"

	if err := os.WriteFile(tmpPath, body, 0o600); err != nil {
		w.warn("write sidecar tmp %s: %s", tmpPath, err)

		return
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		w.warn("rename sidecar %s: %s", finalPath, err)

		return
	}
}

func (w *sidecarWriter) warn(format string, args ...any) {
	if w.logger == nil {
		return
	}

	w.logger.Warnf("sidecar writer: "+format, args...)
}

// sidecarFilename is pid-closedTsNs-uuid.json so a lexical sort groups by
// pid and tails the newest without reading content.
func sidecarFilename(s SessionSidecar) string {
	return fmt.Sprintf("%d-%d-%s.json", s.PeerPID, s.ClosedAt.UnixNano(), s.SidecarUUID)
}

// newSessionSidecar materialises a sidecar from a per-conn session plus the
// outcome of the current flush. Peer-ancestry resolution is injected so
// callers can swap in a fake in tests.
func newSessionSidecar(conn *connectionSession, closedAt time.Time, resolveAncestry func(pid int) []string) SessionSidecar {
	stats := conn.state.getStats().toPublic()

	return SessionSidecar{
		SchemaVersion:  SidecarSchemaVersion,
		Source:         "proxy",
		SidecarUUID:    uuid.NewString(),
		PeerPID:        conn.peerPID,
		PeerAncestry:   resolveAncestry(conn.peerPID),
		AcceptedAt:     conn.acceptedAt.UTC(),
		ClosedAt:       closedAt.UTC(),
		BlobStats:      stats.BlobStats,
		WrapperSession: nil,
	}
}
