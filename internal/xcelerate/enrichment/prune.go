package enrichment

import (
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// PruneAll runs every enrichment-side startup sweep in one shot: handled
// invocation markers, handled manifest UUIDs, and orphan pending records.
// Called from the proxy entry point; Watcher and Retrier no longer prune on
// their own paths.
func PruneAll(p paths.Paths, now time.Time, logger log.Logger) {
	l := logOr(logger)

	PruneStale(p.XcelerateHandledInvocationDir(), HandledMarkerMaxAge)

	handled := &HandledManifestStore{Path: p.HandledManifestsFile()}
	if err := handled.PruneOlderThan(now, HandledManifestMaxAge); err != nil {
		l.Debugf("PruneAll: handled-manifests prune failed: %s", err)
	}

	pending := &Store{Path: p.PendingInvocationsFile()}
	if err := pending.PruneOrphansOlderThan(now, DefaultRetryMaxAge); err != nil {
		l.Debugf("PruneAll: pending orphan prune failed: %s", err)
	}
}
