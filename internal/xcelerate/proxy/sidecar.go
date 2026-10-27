package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/sessions"
)

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
func (w *sidecarWriter) write(s sessions.Sidecar) {
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
func sidecarFilename(s sessions.Sidecar) string {
	return fmt.Sprintf("%d-%d-%s.json", s.PeerPID, s.ClosedAt.UnixNano(), s.SidecarUUID)
}

// newSessionSidecar materialises a sidecar from a per-conn session plus the
// outcome of the current flush. PeerAncestry names are projected from the
// entries captured at TagConn time — walking ancestry again at ConnEnd time
// would race the peer PID's exit (proc gone → nil names → `peer_ancestry`
// serialises as `null`).
func newSessionSidecar(conn *connectionSession, closedAt time.Time) sessions.Sidecar {
	stats := conn.state.getStats().toPublic()

	var link *sessions.WrapperLink
	if conn.derivedInvocationID != "" {
		link = &sessions.WrapperLink{InvocationID: conn.derivedInvocationID}
	}

	return sessions.Sidecar{
		SchemaVersion:   sessions.SidecarSchemaVersion,
		Source:          "proxy",
		SidecarUUID:     uuid.NewString(),
		PeerPID:         conn.peerPID,
		PeerAncestry:    toNames(conn.ancestryEntries),
		AcceptedAt:      conn.acceptedAt.UTC(),
		ClosedAt:        closedAt.UTC(),
		BlobStats:       stats.BlobStats,
		WrapperSession:  link,
		DerivedDataPath: conn.derivedDataPath,
	}
}
