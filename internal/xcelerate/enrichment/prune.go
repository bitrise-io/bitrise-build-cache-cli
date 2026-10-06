package enrichment

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// SessionSidecarMaxAge bounds how long an unclaimed proxy sidecar sticks
// around before the startup sweep drops it. Lines up with HandledManifestMaxAge
// so both signals age out on the same schedule.
const SessionSidecarMaxAge = 7 * 24 * time.Hour

// PruneAll runs every enrichment-side startup sweep in one shot: handled
// manifest UUIDs, orphan pending records, and stale proxy session sidecars.
func PruneAll(p paths.Paths, now time.Time, logger log.Logger) {
	l := logOr(logger)

	handled := &HandledManifestStore{Path: p.HandledManifestsFile()}
	if err := handled.PruneOlderThan(now, HandledManifestMaxAge); err != nil {
		l.Debugf("PruneAll: handled-manifests prune failed: %s", err)
	}

	pending := &Store{Path: p.PendingInvocationsFile()}
	if _, err := pending.PruneOrphansOlderThan(now, DefaultRetryMaxAge); err != nil {
		l.Debugf("PruneAll: pending orphan prune failed: %s", err)
	}

	if _, err := pruneSessionSidecars(p.XcelerateSessionsDir(), now, SessionSidecarMaxAge); err != nil {
		l.Debugf("PruneAll: session sidecar prune failed: %s", err)
	}
}

// pruneSessionSidecars drops *.json files whose mtime is older than cutoff
// and sweeps leftover *.tmp files (writer-crash artefacts) unconditionally.
// Missing dir is a no-op — the proxy may not have run yet.
func pruneSessionSidecars(dir string, now time.Time, maxAge time.Duration) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}

		return 0, err //nolint:wrapcheck // the single caller logs with context
	}

	cutoff := now.Add(-maxAge)
	pruned := 0

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()
		switch {
		case strings.HasSuffix(name, ".tmp"):
			_ = os.Remove(filepath.Join(dir, name))
			pruned++
		case strings.HasSuffix(name, ".json"):
			info, infoErr := e.Info()
			if infoErr != nil {
				continue
			}

			if info.ModTime().Before(cutoff) {
				_ = os.Remove(filepath.Join(dir, name))
				pruned++
			}
		}
	}

	return pruned, nil
}
