package enrichment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/sessions"
)

// supportedSidecarSchema matches sessions.SidecarSchemaVersion; higher values
// are left on disk for a newer binary to consume.
const supportedSidecarSchema = sessions.SidecarSchemaVersion

// expectedAncestor must appear in a sidecar's PeerAncestry list to qualify.
const expectedAncestor = "xcodebuild"

// SidecarReader resolves sidecars left by the proxy that overlap a manifest
// group's time window. Returns the representative blob snapshot (largest-op
// winner across overlapping sidecars), the sidecar paths to unlink on PUT, and
// the derived InvocationID carried by the winning sidecar's WrapperLink (empty
// when the proxy could not anchor peer ancestry).
type SidecarReader interface {
	Lookup(group ManifestEntryGroup) (snapshot *blobstats.Snapshot, consumed []string, invocationID string, found bool)

	// OrphanSidecarsGroupedByInvocation returns sidecars older than minAge whose
	// WrapperSession.InvocationID is populated, grouped by that InvocationID.
	// Ignores manifest correlation entirely — the result is "orphan-eligible":
	// callers synthesize a pseudo-group so the enricher can emit a wrapperless
	// PUT when no LogStoreManifest.plist ever arrived.
	OrphanSidecarsGroupedByInvocation(now time.Time, minAge time.Duration) map[string]SidecarGroup
}

// SidecarGroup aggregates one or more sidecars sharing an InvocationID.
// Blob stats are summed across the set; Start/Stop collapse to earliest/latest.
type SidecarGroup struct {
	Blob             *blobstats.Snapshot
	Start, Stop      time.Time
	DerivedDataPaths []string
	SidecarUUIDs     []string
	Paths            []string
}

type sidecarIndex struct {
	dir    string
	logger log.Logger
}

// NewSidecarIndex returns a SidecarReader backed by dir. A reader fresh-reads
// per Lookup; no caching.
func NewSidecarIndex(dir string, logger log.Logger) SidecarReader {
	return &sidecarIndex{dir: dir, logger: logger}
}

func (i *sidecarIndex) Lookup(group ManifestEntryGroup) (*blobstats.Snapshot, []string, string, bool) {
	logger := logOr(i.logger)

	if i.dir == "" {
		logger.Debugf("Sidecar index skipped: empty dir")

		return nil, nil, "", false
	}

	entries, err := os.ReadDir(i.dir)
	if err != nil {
		logger.Debugf("Sidecar index read %s: %s", i.dir, err)

		return nil, nil, "", false
	}

	groupStart := group.Start()
	groupStop := group.Stop()

	var (
		best           *blobstats.Snapshot
		bestBlobTotal  int64
		bestInvocation string
		versionSkipped bool
	)
	consumedPaths := make([]string, 0, len(entries))

	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}

		path := filepath.Join(i.dir, de.Name())
		s, ok := loadSidecar(path, logger, &versionSkipped)
		if !ok {
			continue
		}
		if !overlaps(s.AcceptedAt, s.ClosedAt, groupStart, groupStop) {
			continue
		}

		if blob := s.BlobStats; blob != nil {
			total := blob.Download.OpCount + blob.Download.MissCount + blob.Upload.OpCount
			if best == nil || total > bestBlobTotal {
				best = blob
				bestBlobTotal = total
				bestInvocation = ""
				if s.WrapperSession != nil {
					bestInvocation = s.WrapperSession.InvocationID
				}
			}
		}

		consumedPaths = append(consumedPaths, path)
	}

	if len(consumedPaths) == 0 {
		return nil, nil, "", false
	}

	return best, consumedPaths, bestInvocation, true
}

func (i *sidecarIndex) OrphanSidecarsGroupedByInvocation(now time.Time, minAge time.Duration) map[string]SidecarGroup {
	logger := logOr(i.logger)

	if i.dir == "" {
		return nil
	}

	entries, err := os.ReadDir(i.dir)
	if err != nil {
		logger.Debugf("Sidecar index read %s: %s", i.dir, err)

		return nil
	}

	var versionSkipped bool
	groups := make(map[string]SidecarGroup)

	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}

		path := filepath.Join(i.dir, de.Name())
		s, ok := loadSidecar(path, logger, &versionSkipped)
		if !ok {
			continue
		}
		if s.WrapperSession == nil || s.WrapperSession.InvocationID == "" {
			continue
		}
		if s.ClosedAt.IsZero() || now.Sub(s.ClosedAt) <= minAge {
			continue
		}

		g := groups[s.WrapperSession.InvocationID]
		g.Blob = mergeBlobs(g.Blob, s.BlobStats)
		if g.Start.IsZero() || (!s.AcceptedAt.IsZero() && s.AcceptedAt.Before(g.Start)) {
			g.Start = s.AcceptedAt
		}
		if s.ClosedAt.After(g.Stop) {
			g.Stop = s.ClosedAt
		}
		if s.SidecarUUID != "" {
			g.SidecarUUIDs = append(g.SidecarUUIDs, s.SidecarUUID)
		}
		g.Paths = append(g.Paths, path)
		groups[s.WrapperSession.InvocationID] = g
	}

	return groups
}

// mergeBlobs sums the per-direction counters (op / miss / error / skipped-saved
// / bytes) of two snapshots. Histogram and throughput retained-sample fields
// are intentionally dropped — the pseudo-group path only needs totals for the
// hit-rate fallback and the analytics counters.
func mergeBlobs(a, b *blobstats.Snapshot) *blobstats.Snapshot {
	if a == nil && b == nil {
		return nil
	}
	if a == nil {
		cp := *b

		return &cp
	}
	if b == nil {
		return a
	}

	a.Upload.OpCount += b.Upload.OpCount
	a.Upload.ErrorCount += b.Upload.ErrorCount
	a.Upload.MissCount += b.Upload.MissCount
	a.Upload.SkippedAlreadySavedCount += b.Upload.SkippedAlreadySavedCount
	a.Upload.BytesTotal += b.Upload.BytesTotal

	a.Download.OpCount += b.Download.OpCount
	a.Download.ErrorCount += b.Download.ErrorCount
	a.Download.MissCount += b.Download.MissCount
	a.Download.SkippedAlreadySavedCount += b.Download.SkippedAlreadySavedCount
	a.Download.BytesTotal += b.Download.BytesTotal

	return a
}

// loadSidecar reads + unmarshals + gates on schema / ancestry. Returns ok=false
// (sidecar must be skipped) for any filter miss; versionSkipped tracks the
// "logged once per Lookup" promise for schema-too-new.
func loadSidecar(path string, logger log.Logger, versionSkipped *bool) (sessions.Sidecar, bool) {
	body, err := os.ReadFile(path)
	if err != nil {
		logger.Debugf("Sidecar read %s: %s", path, err)

		return sessions.Sidecar{}, false
	}

	var s sessions.Sidecar
	if err := json.Unmarshal(body, &s); err != nil {
		logger.Debugf("Sidecar unmarshal %s: %s", path, err)

		return sessions.Sidecar{}, false
	}

	if s.SchemaVersion > supportedSidecarSchema {
		if !*versionSkipped {
			logger.Debugf("Sidecar %s schema_version=%d > %d, leaving on disk", path, s.SchemaVersion, supportedSidecarSchema)
			*versionSkipped = true
		}

		return sessions.Sidecar{}, false
	}

	if !ancestryMatches(s.PeerAncestry) {
		logger.Debugf("Sidecar %s ancestry rejected: %v", path, s.PeerAncestry)

		return sessions.Sidecar{}, false
	}

	return s, true
}

// ---------------------------------------------------------------------------
// Private
// ---------------------------------------------------------------------------

func ancestryMatches(ancestry []string) bool {
	for _, a := range ancestry {
		if a == expectedAncestor {
			return true
		}
	}

	return false
}

// overlaps uses the half-open window [AcceptedAt, ClosedAt) ∩ [groupStart, groupStop).
// Any zero timestamp disqualifies the sidecar.
func overlaps(accepted, closed, groupStart, groupStop time.Time) bool {
	if accepted.IsZero() || closed.IsZero() || groupStart.IsZero() || groupStop.IsZero() {
		return false
	}

	return accepted.Before(groupStop) && groupStart.Before(closed)
}
