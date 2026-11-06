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

// LookupFilter tightens sidecar-to-group matching with DerivedData and
// build-description-hash equality. Both axes follow the same rule: empty on
// either side (filter or sidecar) skips that axis and falls back to the
// pre-existing ancestry + time-overlap match. This is required so IDE flows
// (which may not carry a DD path on every sidecar, and whose early sidecars
// can flush before the build-description hash is known) and the orphan
// scan path (which has no manifest to source DD from) do not regress.
type LookupFilter struct {
	// DerivedData is the DD root parsed from the group's manifest path; empty
	// on sidecar-only orphan paths. The filter is honored only when both this
	// and the sidecar's DerivedDataPath are non-empty.
	DerivedData string
	// Hashes is the set of acceptable build-description hashes (today: one,
	// aggregated from the group's manifest entries by the Enricher). A
	// non-empty slice is required to activate the hex axis; a sidecar whose
	// BuildRequestHex is empty is NOT filtered out — the sidecar simply does
	// not have the information to disambiguate.
	Hashes []string
}

// SidecarReader resolves sidecars left by the proxy that overlap a manifest
// group's time window. Returns the representative blob snapshot (largest-op
// winner across overlapping sidecars), the sidecar paths to unlink on PUT, and
// the derived InvocationID carried by the winning sidecar's WrapperLink (empty
// when the proxy could not anchor peer ancestry).
//
// filter narrows the candidate set beyond ancestry + time overlap. Empty
// LookupFilter (zero value) preserves pre-existing behavior.
type SidecarReader interface {
	Lookup(group ManifestEntryGroup, filter LookupFilter) (snapshot *blobstats.Snapshot, consumed []string, invocationID string, found bool)

	// Has returns true when a sidecar overlapping the group's time window exists
	// on disk with a non-empty WrapperSession.InvocationID. Side-effect free —
	// used by the watcher's retry bucket to short-circuit orphan-mint without
	// consuming sidecar paths. MaxCorrelationRetries is the sole safety net
	// against a group deferring forever if the matching sidecar never arrives;
	// Has itself has no bound.
	Has(group ManifestEntryGroup, filter LookupFilter) bool

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
// BuildRequestHex is the hex of the earliest-accepted sidecar in the group —
// typically the Primary that generated the reseat-anchored WrapperLink. Used
// by the enricher's hex-bridge to match a pseudo-group back to the originating
// manifest's recorded build-description hash.
type SidecarGroup struct {
	Blob             *blobstats.Snapshot
	Start, Stop      time.Time
	DerivedDataPaths []string
	SidecarUUIDs     []string
	Paths            []string
	BuildRequestHex  string
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

func (i *sidecarIndex) Lookup(group ManifestEntryGroup, filter LookupFilter) (*blobstats.Snapshot, []string, string, bool) {
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

	agg := newInvocationAggregator(logger)
	anchorless := make([]sidecarMatch, 0, len(entries))
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
		if !overlapsClosed(s.AcceptedAt, s.ClosedAt, groupStart, groupStop) {
			continue
		}
		// DerivedData + hex equality tighten disambiguation for parallel builds.
		// Both axes honor the empty-means-skip rule — see LookupFilter docs.
		if filter.DerivedData != "" && s.DerivedDataPath != "" && filter.DerivedData != s.DerivedDataPath {
			continue
		}
		if len(filter.Hashes) > 0 && s.BuildRequestHex != "" && !hexMatches(s.BuildRequestHex, filter.Hashes) {
			continue
		}

		if invocationIDOf(s) != "" {
			agg.add(path, s)

			continue
		}
		anchorless = append(anchorless, sidecarMatch{path: path, blob: s.BlobStats})
	}

	if len(agg.groups) == 0 && len(anchorless) == 0 {
		return nil, nil, "", false
	}

	snap, invocationID, consumed := pickLookupWinner(agg, anchorless)

	return snap, consumed, invocationID, true
}

// sidecarMatch is the anchorless-path book-keeping: Lookup needs to remember
// the path for unlink and the blob for the anchorless-only winner fallback.
type sidecarMatch struct {
	path string
	blob *blobstats.Snapshot
}

// scoredGroup is the tuple pickLookupWinner ranks: invoID + its finalised
// SidecarGroup + the pre-computed op-total used as the primary sort key.
type scoredGroup struct {
	invoID string
	group  SidecarGroup
	total  int64
}

// pickLookupWinner selects the winning blob/InvocationID across aggregated
// groups and anchorless sidecars.
//
//   - Any anchored group with a non-zero op-total: largest aggregated total
//     wins (sum of Download.OpCount + Download.MissCount + Upload.OpCount);
//     ties by earliest AcceptedAt, then by firstAnchored order.
//   - No stats-bearing anchored groups but at least one zero-total anchored
//     group: first-seen anchored InvocationID wins so the nil-BlobStats
//     InvocationID still surfaces to the enricher (regression guard
//     TestSidecarIndex_Lookup_FirstSeenInvocationIDWhenAllNilBlobStats).
//   - No anchored groups at all: largest-op anchorless blob wins (pre-reducer
//     TestSidecarIndex_TwoMatchesPickLargestBlob semantics).
//
// Every consumed sidecar path (both aggregated and anchorless) is returned so
// the enricher unlinks the full set.
func pickLookupWinner(agg *invocationAggregator, anchorless []sidecarMatch) (*blobstats.Snapshot, string, []string) {
	finalised := agg.finalise()
	consumed := make([]string, 0, len(anchorless))

	var statsBearing, zeroTotal []scoredGroup
	for invoID, g := range finalised {
		consumed = append(consumed, g.Paths...)
		total := aggregateOpTotal(g.Blob)
		entry := scoredGroup{invoID: invoID, group: g, total: total}
		if g.Blob != nil && total > 0 {
			statsBearing = append(statsBearing, entry)
		} else {
			zeroTotal = append(zeroTotal, entry)
		}
	}
	for _, m := range anchorless {
		consumed = append(consumed, m.path)
	}

	switch {
	case len(statsBearing) > 0:
		winner := pickLargestGroup(statsBearing, agg.firstAnchored, agg.earliestAccepted)

		return winner.group.Blob, winner.invoID, consumed
	case len(zeroTotal) > 0:
		winner := pickFirstAnchoredGroup(zeroTotal, agg.firstAnchored)

		return winner.group.Blob, winner.invoID, consumed
	default:
		return pickAnchorlessWinner(anchorless), "", consumed
	}
}

func aggregateOpTotal(b *blobstats.Snapshot) int64 {
	if b == nil {
		return 0
	}

	return b.Download.OpCount + b.Download.MissCount + b.Upload.OpCount
}

func pickLargestGroup(entries []scoredGroup, firstAnchored map[string]int, earliestAccepted map[string]time.Time) scoredGroup {
	best := entries[0]
	for _, e := range entries[1:] {
		if isBetterStatsBearing(e, best, firstAnchored, earliestAccepted) {
			best = e
		}
	}

	return best
}

func isBetterStatsBearing(candidate, best scoredGroup, firstAnchored map[string]int, earliestAccepted map[string]time.Time) bool {
	switch {
	case candidate.total != best.total:
		return candidate.total > best.total
	case !earliestAccepted[candidate.invoID].Equal(earliestAccepted[best.invoID]):
		return earliestAccepted[candidate.invoID].Before(earliestAccepted[best.invoID])
	default:
		return firstAnchored[candidate.invoID] < firstAnchored[best.invoID]
	}
}

// pickFirstAnchoredGroup: when all groups have zero op-total, the first-added
// InvocationID wins — this preserves the pre-reducer first-seen semantics for
// nil-BlobStats sidecars.
func pickFirstAnchoredGroup(entries []scoredGroup, firstAnchored map[string]int) scoredGroup {
	best := entries[0]
	for _, e := range entries[1:] {
		if firstAnchored[e.invoID] < firstAnchored[best.invoID] {
			best = e
		}
	}

	return best
}

// pickAnchorlessWinner preserves the pre-reducer "largest-op anchorless blob
// wins" tiebreak. Only reached when no anchored group was seen — the common
// path for sidecars whose proxy peer-ancestry gate rejected the anchor.
func pickAnchorlessWinner(anchorless []sidecarMatch) *blobstats.Snapshot {
	var (
		best      *blobstats.Snapshot
		bestTotal int64
	)
	for _, m := range anchorless {
		if m.blob == nil {
			continue
		}
		total := aggregateOpTotal(m.blob)
		if best == nil || total > bestTotal {
			best = m.blob
			bestTotal = total
		}
	}

	return best
}

// Has walks i.dir once and returns true on the first sidecar whose
// AcceptedAt/ClosedAt window overlaps the group AND whose
// WrapperSession.InvocationID is non-empty. No file is unlinked or recorded.
// Honors the same empty-means-skip rule as Lookup on DerivedData + hex.
func (i *sidecarIndex) Has(group ManifestEntryGroup, filter LookupFilter) bool {
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
		if filter.DerivedData != "" && s.DerivedDataPath != "" && filter.DerivedData != s.DerivedDataPath {
			continue
		}
		if len(filter.Hashes) > 0 && s.BuildRequestHex != "" && !hexMatches(s.BuildRequestHex, filter.Hashes) {
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

	agg := newInvocationAggregator(logger)

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
		if s.ClosedAt.IsZero() || now.Sub(s.ClosedAt) <= minAge {
			continue
		}

		agg.add(path, s)
	}

	// Aged sidecars without an anchored InvocationID will never surface as
	// enriched invocations — the proxy's peer-ancestry gate silently dropped
	// them. Warn-level because at this point it's not a transient condition.
	if agg.agedWithoutInvoID > 0 {
		logger.Warnf("Enrichment: %d aged sidecar(s) have no WrapperSession.InvocationID — their cache stats will not land in BE", agg.agedWithoutInvoID)
	}

	return agg.finalise()
}

// invocationAggregator reduces sidecars into per-InvocationID SidecarGroups.
// Shared by Lookup (manifest-correlated path) and
// OrphanSidecarsGroupedByInvocation (wrapperless pseudo-group path). Keyed on
// WrapperSession.InvocationID; sidecars without one must be filtered by the
// caller before `add` — only the orphan path increments agedWithoutInvoID.
//
// BuildStartedAt supersedes AcceptedAt only for pseudo-group derivation;
// Lookup/Has still read AcceptedAt. Earliest BuildStartedAt and earliest
// AcceptedAt are tracked separately so a legacy sidecar's daemon-start
// AcceptedAt cannot under-cut an upgraded sidecar's per-build
// BuildStartedAt in the same group. hexAcceptedAt tracks the earliest
// AcceptedAt per group independently from Start so the hex still comes from
// the first-accepted sidecar (deterministic across scan ticks). firstAnchored
// records the add-order of the first sidecar seen under each InvocationID; it
// is the fallback tiebreak when every anchored group has zero op-total.
type invocationAggregator struct {
	groups             map[string]SidecarGroup
	earliestBuildStart map[string]time.Time
	earliestAccepted   map[string]time.Time
	hexAcceptedAt      map[string]time.Time
	firstAnchored      map[string]int
	nextOrder          int
	agedWithoutInvoID  int
	logger             log.Logger
}

func newInvocationAggregator(logger log.Logger) *invocationAggregator {
	return &invocationAggregator{
		groups:             make(map[string]SidecarGroup),
		earliestBuildStart: make(map[string]time.Time),
		earliestAccepted:   make(map[string]time.Time),
		hexAcceptedAt:      make(map[string]time.Time),
		firstAnchored:      make(map[string]int),
		logger:             logger,
	}
}

func (a *invocationAggregator) add(path string, s sessions.Sidecar) {
	if s.WrapperSession == nil || s.WrapperSession.InvocationID == "" {
		a.agedWithoutInvoID++

		return
	}

	invoID := s.WrapperSession.InvocationID
	if _, present := a.firstAnchored[invoID]; !present {
		a.firstAnchored[invoID] = a.nextOrder
		a.nextOrder++
	}
	g := a.groups[invoID]
	g.Blob = mergeBlobs(g.Blob, s.BlobStats, a.logger)

	a.updateEarliest(invoID, s)
	a.updateHex(&g, invoID, s)

	if s.ClosedAt.After(g.Stop) {
		g.Stop = s.ClosedAt
	}
	if s.SidecarUUID != "" {
		g.SidecarUUIDs = append(g.SidecarUUIDs, s.SidecarUUID)
	}
	g.Paths = append(g.Paths, path)
	appendUniqueDDPath(&g, s.DerivedDataPath)
	a.groups[invoID] = g
}

// appendUniqueDDPath keeps SidecarGroup.DerivedDataPaths deduped per-group so
// the enricher's plist-boundary bridge (PR596 S7) can resolve each orphan
// InvocationID back to the DD subtree whose Logs/Build/LogStoreManifest.plist
// carries its enclosing master.
func appendUniqueDDPath(g *SidecarGroup, dd string) {
	if dd == "" {
		return
	}

	for _, existing := range g.DerivedDataPaths {
		if existing == dd {
			return
		}
	}

	g.DerivedDataPaths = append(g.DerivedDataPaths, dd)
}

func (a *invocationAggregator) updateEarliest(invoID string, s sessions.Sidecar) {
	if !s.BuildStartedAt.IsZero() {
		if cur, present := a.earliestBuildStart[invoID]; !present || s.BuildStartedAt.Before(cur) {
			a.earliestBuildStart[invoID] = s.BuildStartedAt
		}
	}
	if !s.AcceptedAt.IsZero() {
		if cur, present := a.earliestAccepted[invoID]; !present || s.AcceptedAt.Before(cur) {
			a.earliestAccepted[invoID] = s.AcceptedAt
		}
	}
}

func (a *invocationAggregator) updateHex(g *SidecarGroup, invoID string, s sessions.Sidecar) {
	// Earliest-accepted sidecar supplies the hex: later flushes under the
	// same InvocationID (idle beats + terminal close) share the hex that
	// identified the originating ⌘B, so any of them is safe to use — but
	// picking the earliest is deterministic across scan ticks.
	cur, present := a.hexAcceptedAt[invoID]
	if !present || (!s.AcceptedAt.IsZero() && s.AcceptedAt.Before(cur)) {
		a.hexAcceptedAt[invoID] = s.AcceptedAt
		if s.BuildRequestHex != "" {
			g.BuildRequestHex = s.BuildRequestHex
		}

		return
	}
	if g.BuildRequestHex == "" && s.BuildRequestHex != "" {
		g.BuildRequestHex = s.BuildRequestHex
	}
}

// finalise applies the BuildStartedAt-first precedence: any sidecar in the
// group that carries BuildStartedAt wins over all AcceptedAt values.
func (a *invocationAggregator) finalise() map[string]SidecarGroup {
	for invoID, g := range a.groups {
		switch {
		case !a.earliestBuildStart[invoID].IsZero():
			g.Start = a.earliestBuildStart[invoID]
		case !a.earliestAccepted[invoID].IsZero():
			g.Start = a.earliestAccepted[invoID]
		}
		a.groups[invoID] = g
	}

	return a.groups
}

// mergeBlobs sums every field — counters AND histograms — of two snapshots
// via blobstats.MergeDirectionSnapshots. CAS/KV sub-protocol snapshots merge
// pairwise when both sides carry them; otherwise the non-nil side wins.
// Throughput p10/p50/p90 are re-derived from the merged histogram buckets
// because raw per-sample throughput doesn't travel on the wire.
func mergeBlobs(a, b *blobstats.Snapshot, logger log.Logger) *blobstats.Snapshot {
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

	a.Download = blobstats.MergeDirectionSnapshots(a.Download, b.Download, logger)
	a.Upload = blobstats.MergeDirectionSnapshots(a.Upload, b.Upload, logger)
	a.CAS = mergeProtocolSnapshots(a.CAS, b.CAS, logger)
	a.KV = mergeProtocolSnapshots(a.KV, b.KV, logger)

	return a
}

func mergeProtocolSnapshots(a, b *blobstats.ProtocolSnapshot, logger log.Logger) *blobstats.ProtocolSnapshot {
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

	return &blobstats.ProtocolSnapshot{
		Upload:   blobstats.MergeDirectionSnapshots(a.Upload, b.Upload, logger),
		Download: blobstats.MergeDirectionSnapshots(a.Download, b.Download, logger),
	}
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

// hexMatches reports whether sidecarHex equals any of hashes. Both sides are
// compared verbatim — Xcode's BuildDescriptionHash is already lowercase hex, so
// no normalisation is applied. Shared by Lookup / Has (DD+hex filter axis) and
// the Watcher's hex-bridge path so the equality contract has a single owner.
func hexMatches(sidecarHex string, hashes []string) bool {
	if sidecarHex == "" {
		return false
	}

	for _, h := range hashes {
		if h == sidecarHex {
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
