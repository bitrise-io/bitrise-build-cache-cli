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

// sidecarIdentity bundles the attributes that identify the row the sidecar
// contributes to: the InvocationID the BE row is keyed under and the
// build-request hex that generated the accumulated blob stats. On reseat
// both must snapshot the OLD values before the swap — otherwise the flushed
// stats land on the freshly-derived ID / new hex.
type sidecarIdentity struct {
	InvocationID    string
	BuildRequestHex string
}

// newSessionSidecar materialises a sidecar from a per-conn session plus the
// state slice to serialise. The slice is passed explicitly (not read off conn)
// so intermediate flushes can hand in the just-swapped-out state while the
// connection goes on recording into a fresh one. PeerAncestry names are
// projected from the entries captured at TagConn time — walking ancestry
// again at close time would race the peer PID's exit (proc gone → nil names
// → `peer_ancestry` serialises as `null`).
//
// identity is the (InvocationID, BuildRequestHex) tuple the sidecar is
// attributed to: the reseat-flush path passes the pre-swap pair so the
// accumulated state lands on the old row rather than the newly-derived one;
// idle/terminal paths pass the current pair.
func newSessionSidecar(conn *connectionSession, state *sessionState, closedAt time.Time, terminal bool, identity sidecarIdentity) sessions.Sidecar {
	stats := state.getStats().toPublic()

	var link *sessions.WrapperLink
	if identity.InvocationID != "" {
		link = &sessions.WrapperLink{InvocationID: identity.InvocationID}
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
		BuildRequestHex: identity.BuildRequestHex,
		Terminal:        terminal,
	}
}
