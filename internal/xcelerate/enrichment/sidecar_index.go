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

// acceptedAncestors are process names that qualify a sidecar for enrichment.
// "xcodebuild" is the CLI/CI flow; "Xcode" is the IDE flow (peer ancestry
// resolves to [SWBBuildService, Xcode] because SwiftBuild hosts the plugin
// out of process).
//
//nolint:gochecknoglobals // immutable set, equivalent to const.
var acceptedAncestors = map[string]struct{}{
	"xcodebuild": {},
	"Xcode":      {},
}

// SidecarReader resolves sidecars left by the proxy that overlap a manifest
// group's time window. Returns the representative blob snapshot (largest-op
// winner across overlapping sidecars), the sidecar paths to unlink on PUT, and
// the derived InvocationID carried by the winning sidecar's WrapperLink (empty
// when the proxy could not anchor peer ancestry).
type SidecarReader interface {
	Lookup(group ManifestEntryGroup) (snapshot *blobstats.Snapshot, consumed []string, invocationID string, found bool)

	// Has returns true when a sidecar overlapping the group's time window exists
	// on disk with a non-empty WrapperSession.InvocationID. Side-effect free —
	// used by the watcher's retry bucket to short-circuit orphan-mint without
	// consuming sidecar paths.
	Has(group ManifestEntryGroup) bool

	// OrphanSidecarsGroupedByInvocation returns sidecars older than minAge whose
	// WrapperSession.InvocationID is populated, grouped by that InvocationID.
	// Ignores manifest correlation entirely — the result is "orphan-eligible":
	// callers synthesize a pseudo-group so the enricher can emit a wrapperless
	// PUT when no LogStoreManifest.plist ever arrived.
	OrphanSidecarsGroupedByInvocation(now time.Time, minAge time.Duration) map[string]SidecarGroup

	// PendingDerivedDataPaths returns the deduped set of DerivedDataPath values
	// recorded in sidecars currently on disk. Zero entries when no sidecar
	// carries a DD path. The enrichment watcher uses this to glob
	// project-local DerivedData roots that live outside the HOME-relative
	// defaults.
	PendingDerivedDataPaths() []string
	// PendingCount returns the number of sidecar files currently on disk,
	// regardless of ancestry gate or DD path presence. Used by the watcher to
	// detect the "0 manifests, N uncorrelated sidecars" diagnostic that fires
	// when enrichment stalls because no DerivedData path is observable.
	PendingCount() int
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

func invocationIDOf(s sessions.Sidecar) string {
	if s.WrapperSession == nil {
		return ""
	}

	return s.WrapperSession.InvocationID
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
		if !overlapsClosed(s.AcceptedAt, s.ClosedAt, groupStart, groupStop) {
			continue
		}

		// Prefer-stats-bearing: stats-winner InvocationID always wins; nil-BlobStats first-seen is the fallback.
		sidecarInvocation := invocationIDOf(s)

		if blob := s.BlobStats; blob != nil {
			total := blob.Download.OpCount + blob.Download.MissCount + blob.Upload.OpCount
			if best == nil || total > bestBlobTotal {
				best = blob
				bestBlobTotal = total
				bestInvocation = sidecarInvocation
			}
		} else if best == nil && bestInvocation == "" && sidecarInvocation != "" {
			bestInvocation = sidecarInvocation
		}

		consumedPaths = append(consumedPaths, path)
	}

	if len(consumedPaths) == 0 {
		return nil, nil, "", false
	}

	return best, consumedPaths, bestInvocation, true
}

// Has walks i.dir once and returns true on the first sidecar whose
// AcceptedAt/ClosedAt window overlaps the group AND whose
// WrapperSession.InvocationID is non-empty. No file is unlinked or recorded.
func (i *sidecarIndex) Has(group ManifestEntryGroup) bool {
	logger := logOr(i.logger)

	if i.dir == "" {
		return false
	}

	entries, err := os.ReadDir(i.dir)
	if err != nil {
		logger.Debugf("Sidecar index read %s for Has: %s", i.dir, err)

		return false
	}

	groupStart := group.Start()
	groupStop := group.Stop()

	var versionSkipped bool
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}

		path := filepath.Join(i.dir, de.Name())
		s, ok := loadSidecar(path, logger, &versionSkipped)
		if !ok {
			continue
		}
		if !overlapsPotential(s.AcceptedAt, s.ClosedAt, groupStart, groupStop) {
			continue
		}
		if s.WrapperSession == nil || s.WrapperSession.InvocationID == "" {
			continue
		}

		return true
	}

	return false
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

	var (
		versionSkipped    bool
		agedWithoutInvoID int
	)
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
		if s.ClosedAt.IsZero() || now.Sub(s.ClosedAt) <= minAge {
			continue
		}
		if s.WrapperSession == nil || s.WrapperSession.InvocationID == "" {
			agedWithoutInvoID++

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

	// Aged sidecars without an anchored InvocationID will never surface as
	// enriched invocations — the proxy's peer-ancestry gate silently dropped
	// them. Warn-level because at this point it's not a transient condition.
	if agedWithoutInvoID > 0 {
		logger.Warnf("Enrichment: %d aged sidecar(s) have no WrapperSession.InvocationID — their cache stats will not land in BE", agedWithoutInvoID)
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

// PendingDerivedDataPaths reads every sidecar on disk and returns the deduped
// non-empty DerivedDataPath set. Unlike Lookup, this does not filter by
// ancestry — a sidecar with no xcodebuild anchor can still legitimately carry
// a DD path when peer resolution partially succeeded. Returns nil when the
// index was constructed without a dir or no sidecar carries a DD path.
func (i *sidecarIndex) PendingDerivedDataPaths() []string {
	logger := logOr(i.logger)

	if i.dir == "" {
		return nil
	}

	entries, err := os.ReadDir(i.dir)
	if err != nil {
		logger.Debugf("Sidecar index read %s for DD paths: %s", i.dir, err)

		return nil
	}

	seen := make(map[string]struct{})
	paths := make([]string, 0, len(entries))
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}

		path := filepath.Join(i.dir, de.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var s sessions.Sidecar
		if err := json.Unmarshal(body, &s); err != nil {
			continue
		}

		if s.DerivedDataPath == "" {
			continue
		}
		if _, dup := seen[s.DerivedDataPath]; dup {
			continue
		}

		seen[s.DerivedDataPath] = struct{}{}
		paths = append(paths, s.DerivedDataPath)
	}

	return paths
}

// PendingCount counts JSON sidecar files currently on disk. Fast path: no
// unmarshal, no ancestry gate. Returns zero on read errors (consistent with
// the rest of the index — the signal exists for diagnostics, not correctness).
func (i *sidecarIndex) PendingCount() int {
	if i.dir == "" {
		return 0
	}

	entries, err := os.ReadDir(i.dir)
	if err != nil {
		return 0
	}

	n := 0
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}

		n++
	}

	return n
}

// Private

func ancestryMatches(ancestry []string) bool {
	for _, a := range ancestry {
		if _, ok := acceptedAncestors[a]; ok {
			return true
		}
	}

	return false
}

// overlapsClosed uses the half-open window [AcceptedAt, ClosedAt) ∩ [groupStart, groupStop).
// Any zero timestamp disqualifies the sidecar — used by Lookup, which reads
// stats and so must wait for a closed, final sidecar.
func overlapsClosed(accepted, closed, groupStart, groupStop time.Time) bool {
	if accepted.IsZero() || closed.IsZero() || groupStart.IsZero() || groupStop.IsZero() {
		return false
	}

	return accepted.Before(groupStop) && groupStart.Before(closed)
}

// overlapsPotential answers "could this (possibly-open) sidecar still correlate
// to the group?" — used by Has to defer the retry bucket while a sidecar is
// still open. An open sidecar whose AcceptedAt sits before the group's stop
// counts: its final ClosedAt will land somewhere after now, so the window
// overlap can only be settled once the sidecar closes.
func overlapsPotential(accepted, closed, groupStart, groupStop time.Time) bool {
	if accepted.IsZero() || groupStart.IsZero() || groupStop.IsZero() {
		return false
	}
	if accepted.After(groupStop) {
		return false
	}
	if !closed.IsZero() {
		return overlapsClosed(accepted, closed, groupStart, groupStop)
	}

	return true
}
