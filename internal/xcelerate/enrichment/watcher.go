package enrichment

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcactivitylog"
)

const DefaultPollInterval = 5 * time.Second

const DefaultDerivedDataGlob = "Library/Developer/Xcode/DerivedData/*/Logs/*/LogStoreManifest.plist"

const DefaultMaxCorrelationRetries = 6

// RecentEmissionsRetention bounds how long a manifest-emission is kept in
// Watcher.recentEmissions so scanOrphanSidecars can dedup a pseudo-group PUT
// across scan ticks. Covers the typical manifest-orphan-mint → sidecar-flush
// gap on the IDE flow (orphan mints ~30s after first sighting; sidecar flushes
// seconds-to-minutes after build end).
const RecentEmissionsRetention = 10 * time.Minute

type Watcher struct {
	HomeDir      string
	Globs        []string
	PollInterval time.Duration
	// Handle receives manifestPath so the callback can resolve the sibling xcactivitylog.
	// With per-entry groups as the default, a prep-Build's skipped PUT no longer
	// shadows the real build — they fuse into one group only when scheme +
	// Command + hex + sub-mergeMaxGap all agree, and otherwise each PUTs under
	// its own UUID. handleGroup marks the group seen unconditionally after Handle.
	Handle func(manifestPath string, group ManifestEntryGroup)
	Logger log.Logger

	// MatchProbe returns true when a pending record for the group
	// exists and Handle would enrich under that record's InvocationID.
	// nil disables the retry bucket: groups fire immediately on first sight.
	MatchProbe            func(group ManifestEntryGroup) bool
	MaxCorrelationRetries int

	// HandledStore persists the seen-UUID set across restarts. nil disables
	// persistence (seen stays in-memory only) — this is the pre-persistence
	// behavior and is still what tests use unless they set the field.
	HandledStore *HandledManifestStore

	// SidecarReader feeds two scan-tail paths: (a) per-scan DerivedDataPath
	// hints turn each unique DD path into a <DD>/Logs/*/LogStoreManifest.plist
	// glob, layered on top of the HOME-relative defaults so a direct
	// `xcodebuild -derivedDataPath ./DerivedData` invocation is still correlated;
	// (b) aged orphan sidecar synthesis builds pseudo-ManifestEntryGroups for
	// InvocationIDs that never produced a LogStoreManifest.plist.
	// Nil disables both — HOME-relative paths remain the fast path, and no
	// synthesis fires.
	SidecarReader SidecarReader

	// Now is a test seam for the timestamp written into HandledStore records.
	// nil falls back to time.Now.
	Now func() time.Time

	// SetMetricsLookup is the Enricher's cache seam. Watcher.scan builds a
	// per-tick cache (keyed by manifestPath + "\x00" + fileName), installs it
	// here before invoking Handle, and clears it at scan tail. Also passed to
	// GroupManifestEntries via WithMetricsLookup so clustering sees the same
	// cache — a log is gunzipped at most once per tick.
	SetMetricsLookup func(MetricsLookup)

	seen    map[string]struct{}
	retries map[string]int

	// tickEmittedIDs collects InvocationIDs the Enricher emitted via the
	// manifest path during the current scan tick so scanOrphanSidecars can
	// dedup. Scoped per tick: reset at scan start, cleared at scan tail.
	tickEmittedIDs map[string]struct{}

	// recentEmissions holds IDs the manifest path just PUT; scanOrphanSidecars
	// consults it to skip clobbering pseudo-group PUTs. Pruned at scan start by
	// RecentEmissionsRetention.
	recentEmissions map[string]emissionRecord

	// lastStalledLogAt rate-limits the "0 manifests + pending sidecars" debug
	// line to one entry per minute.
	lastStalledLogAt time.Time
}

// emissionRecord pairs a manifest-emitted group with the time it was recorded
// so Watcher.recentEmissions can age out stale entries beyond RecentEmissionsRetention.
// BuildDescriptionHash lets scanOrphanSidecars key a hex-exact match against the
// sidecar's recorded build-request hex.
type emissionRecord struct {
	group                ManifestEntryGroup
	emittedAt            time.Time
	BuildDescriptionHash string
}

// RecordEmittedInvocationID is wired as the Enricher's OnEmit hook. It feeds
// the per-tick dedup set that scanOrphanSidecars reads: a sidecar whose ID
// just emitted via the manifest path must not re-emit as a pseudo-group. It
// also stashes the emitted group in recentEmissions so scanOrphanSidecars can
// find an overlapping manifest window and skip clobbering the authoritative row.
// buildDescriptionHash may be empty — hash extraction is best-effort.
func (w *Watcher) RecordEmittedInvocationID(invocationID string, buildDescriptionHash string, group ManifestEntryGroup) {
	if invocationID == "" {
		return
	}

	now := w.now()
	if w.tickEmittedIDs != nil {
		w.tickEmittedIDs[invocationID] = struct{}{}
	}
	if w.recentEmissions != nil {
		w.recentEmissions[invocationID] = emissionRecord{group: group, emittedAt: now, BuildDescriptionHash: buildDescriptionHash}
	}
}

// pruneRecentEmissions drops recentEmissions entries older than
// RecentEmissionsRetention. Called at scan start — the retention window is the
// upper bound on the manifest-orphan-mint → sidecar-flush gap we're willing to
// correlate across.
func (w *Watcher) pruneRecentEmissions() {
	cutoff := w.now().Add(-RecentEmissionsRetention)
	if w.recentEmissions != nil {
		for id, rec := range w.recentEmissions {
			if rec.emittedAt.Before(cutoff) {
				delete(w.recentEmissions, id)
			}
		}
	}
}

// zeroMatchStallInterval caps the "0 manifests across N globs" diagnostic to
// once per minute so a stuck enrichment loop still flags itself without
// flooding the debug log.
const zeroMatchStallInterval = time.Minute

func (w *Watcher) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}

	return time.Now()
}

func (w *Watcher) markHandled(uuid string) {
	w.seen[uuid] = struct{}{}

	if w.HandledStore == nil {
		return
	}

	if err := w.HandledStore.Append(HandledManifest{UUID: uuid, HandledAt: w.now()}); err != nil {
		logOr(w.Logger).Debugf("Persist handled manifest %s failed: %s", uuid, err)
	}
}

func (w *Watcher) markGroupHandled(group ManifestEntryGroup) {
	for _, uuid := range group.UUIDs() {
		w.markHandled(uuid)
	}
}

// groupKey sorts a defensive copy so a future UUIDs() sharing its backing
// array can't mutate the group and so the key survives entry reordering.
func groupKey(group ManifestEntryGroup) string {
	uuids := append([]string(nil), group.UUIDs()...)
	sort.Strings(uuids)

	return strings.Join(uuids, "\x00")
}

func (w *Watcher) Run(ctx context.Context) {
	if w.PollInterval == 0 {
		w.PollInterval = DefaultPollInterval
	}

	w.seen = make(map[string]struct{})
	w.retries = make(map[string]int)
	w.recentEmissions = make(map[string]emissionRecord)

	if !w.seedSeenFromStore() {
		w.scan(true, false)
	}

	ticker := time.NewTicker(w.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.scan(false, false)
		}
	}
}

// ScanOnce runs a single synchronous pass; safe to call on a Watcher that was never Run.
// Used by the shutdown sweep: synthesizes pseudo-groups unconditionally (age gate dropped)
// so wrapperless invocations whose sidecars just closed still land in BE.
func (w *Watcher) ScanOnce() {
	if w.seen == nil {
		w.seen = make(map[string]struct{})
	}
	if w.retries == nil {
		w.retries = make(map[string]int)
	}
	if w.recentEmissions == nil {
		w.recentEmissions = make(map[string]emissionRecord)
	}

	w.scan(false, true)
}

// seedSeenFromStore returns true when HandledStore contributed at least one
// UUID; false lets Run fall back to a silent filesystem seed.
func (w *Watcher) seedSeenFromStore() bool {
	if w.HandledStore == nil {
		return false
	}

	logger := logOr(w.Logger)

	records, err := w.HandledStore.Load()
	if err != nil {
		logger.Debugf("Load handled manifests failed: %s", err)

		return false
	}

	for _, r := range records {
		w.seen[r.UUID] = struct{}{}
	}

	return len(records) > 0
}

func (w *Watcher) scan(seedOnly, forceSynthesize bool) {
	logger := logOr(w.Logger)

	w.tickEmittedIDs = make(map[string]struct{})
	defer func() {
		w.tickEmittedIDs = nil
	}()

	if w.recentEmissions == nil {
		w.recentEmissions = make(map[string]emissionRecord)
	}
	w.pruneRecentEmissions()

	// metricsCache is scoped to this scan tick: a log gunzipped by grouping
	// (hex-aware split) is reused by aggregateHitRate. Cleared at scan tail so
	// no stale metrics leak into the next tick's emission.
	metricsCache := make(map[string]xcactivitylog.Metrics)
	if w.SetMetricsLookup != nil {
		w.SetMetricsLookup(cachedMetricsLookup(metricsCache, "", logger))
		defer w.SetMetricsLookup(nil)
	}

	globs := w.Globs
	if len(globs) == 0 {
		globs = []string{DefaultDerivedDataGlob}
	}

	// HOME-relative defaults go through filepath.Join(HomeDir, ...); sidecar
	// hints are already absolute so they stay as-is.
	type scanTarget struct {
		pattern  string
		absolute bool
	}

	targets := make([]scanTarget, 0, len(globs))
	for _, g := range globs {
		targets = append(targets, scanTarget{pattern: g, absolute: false})
	}
	for _, pattern := range w.sidecarGlobs() {
		targets = append(targets, scanTarget{pattern: pattern, absolute: true})
	}

	totalMatches := 0
	for _, t := range targets {
		pattern := t.pattern
		if !t.absolute {
			pattern = filepath.Join(w.HomeDir, pattern)
		}

		matches, err := filepath.Glob(pattern)
		if err != nil {
			logger.Debugf("LogWatcher glob failed: %s", err)

			continue
		}
		totalMatches += len(matches)

		for _, path := range matches {
			manifestLookup := cachedMetricsLookup(metricsCache, path, logger)

			groups, err := LoadManifestGrouped(path, WithMetricsLookup(manifestLookup))
			if err != nil {
				logger.Debugf("LogWatcher failed to load %s: %s", path, err)

				continue
			}

			// Rebind the Enricher's lookup to this manifest so aggregateHitRate
			// hits the cache under the same (manifestPath, fileName) key.
			if w.SetMetricsLookup != nil {
				w.SetMetricsLookup(manifestLookup)
			}

			for _, group := range groups {
				w.handleGroup(path, group, seedOnly, false)
			}
		}
	}

	if !seedOnly {
		w.scanOrphanSidecars(forceSynthesize)
	}

	w.logStalledIfSuspicious(logger, totalMatches, len(targets))
}

// cachedMetricsLookup wraps a per-tick metricsCache map keyed by
// manifestPath + "\x00" + fileName. The returned lookup is safe to share
// across the grouping pass and aggregateHitRate; each unique key triggers one
// gunzip, repeated lookups serve the cached Metrics. An empty fileName
// short-circuits as OutcomeFileMissing without touching the cache (ignored by
// the callers' FileName guards anyway).
func cachedMetricsLookup(cache map[string]xcactivitylog.Metrics, manifestPath string, logger log.Logger) MetricsLookup {
	return func(entry ManifestEntry) xcactivitylog.Metrics {
		if entry.FileName == "" {
			return xcactivitylog.Metrics{Outcome: xcactivitylog.OutcomeFileMissing}
		}

		key := manifestPath + "\x00" + entry.FileName
		if m, ok := cache[key]; ok {
			return m
		}

		m := xcactivitylog.ReadMetricsFromManifest(manifestPath, entry.FileName, logger)
		cache[key] = m

		return m
	}
}

// scanOrphanSidecars synthesizes pseudo-groups from aged orphan sidecars whose
// WrapperSession.InvocationID is populated and was NOT already emitted via the
// manifest path this tick. forceSynthesize drops the minAge gate — used by the
// shutdown sweep so a sidecar that just closed still lands before exit.
//
// Two emission shapes:
//   - Hex-match: the sidecar's build-request hex matches a manifest emission
//     already recorded this retention window. Clone that manifest group,
//     attach the sidecar blob stats, PUT under the manifest's UUID so the BE
//     overwrite carries a superset of the manifest row.
//   - No match: fall through to a slim SyntheticManifestGroup under the
//     proxy-derived InvocationID (separate BE row).
//
// Sidecars left on disk at this point are ones the manifest-driven Enrich path
// did not consume via SidecarReader.Lookup (consumed sidecars are unlinked
// synchronously inside Handle before this function runs).
func (w *Watcher) scanOrphanSidecars(forceSynthesize bool) {
	if w.SidecarReader == nil {
		return
	}

	logger := logOr(w.Logger)

	pollInterval := w.PollInterval
	if pollInterval == 0 {
		pollInterval = DefaultPollInterval
	}

	minAge := 2 * pollInterval
	if forceSynthesize {
		minAge = 0
	}

	groups := w.SidecarReader.OrphanSidecarsGroupedByInvocation(w.now(), minAge)
	for invocationID, group := range groups {
		if _, emitted := w.tickEmittedIDs[invocationID]; emitted {
			logger.Debugf("Watcher: skip synthetic pseudo-group for InvocationID %s (manifest path emitted this tick)", invocationID)

			continue
		}
		if len(group.SidecarUUIDs) == 0 {
			continue
		}

		if w.tryHexBridgeClone(group, invocationID, logger) {
			continue
		}

		// Shutdown sweep stays sidecar-anchored: a late-closing sidecar's data
		// must land under its own InvocationID, not fold into whatever
		// manifest-orphan happened to fire this scan.
		if !forceSynthesize {
			if _, prior := w.recentEmissions[invocationID]; prior {
				logger.Debugf("Watcher: skip pseudo-group — invocation %s already emitted this retention window (same ⌘B late sidecar)", invocationID)

				continue
			}
		}

		pseudo := SyntheticManifestGroup(group.SidecarUUIDs[0], group.Start, group.Stop, "", "", "")
		logger.Debugf("Watcher: synthesizing pseudo-group from %d sidecar(s) for InvocationID %s", len(group.SidecarUUIDs), invocationID)
		w.handleGroup("", pseudo, false, true)
	}
}

// tryHexBridgeClone dispatches a manifest-UUID-anchored clone when the
// sidecar's build-request hex matches a recent manifest emission AND that
// manifest UUID has not yet been PUT. Returns true when a clone was
// dispatched (caller must continue). Returns false when no match, or when
// the match's manifest UUID is already in recentEmissions — in which case
// the caller falls through to the slim pseudo-group path so residual
// sidecars land under their proxy-derived InvocationID as post-build
// activity, rather than clobbering Path A's blob stats under BE last-write-wins.
func (w *Watcher) tryHexBridgeClone(group SidecarGroup, invocationID string, logger log.Logger) bool {
	if group.BuildRequestHex == "" {
		return false
	}

	emission, ok := w.findEmissionByBuildRequestHex(group.BuildRequestHex, group.Start)
	if !ok {
		return false
	}

	manifestID := emission.group.ManifestUUID()
	if _, alreadyPut := w.recentEmissions[manifestID]; alreadyPut {
		logger.Debugf("Watcher: hex-bridge skipped — manifest UUID %s already PUT; routing residual sidecar to slim pseudo-group path", manifestID)

		return false
	}

	clone := CloneManifestGroupWithSidecar(emission.group, group.Blob, group.Paths)
	logger.Debugf("Watcher: hex-bridged pseudo-group — sidecar hex %s matched manifest UUID %s (InvocationID %s)", group.BuildRequestHex, manifestID, invocationID)
	if w.Handle != nil {
		w.Handle("", clone)
	}

	return true
}

// findEmissionByBuildRequestHex returns the recent emission whose stored
// build-description hash matches hex exactly. When multiple emissions share
// the hex (incremental rebuilds reusing the same build-description), the one
// whose group Start is closest to sidecarAcceptedAt wins — breaks ties by
// temporal proximity so an index-pass reusing a hex does not steal a
// user-build's hex.
func (w *Watcher) findEmissionByBuildRequestHex(hex string, sidecarAcceptedAt time.Time) (emissionRecord, bool) {
	if hex == "" || w.recentEmissions == nil {
		return emissionRecord{}, false
	}

	var (
		best       emissionRecord
		bestDelta  time.Duration = -1
		foundMatch bool
	)

	hashes := []string{hex}
	for _, rec := range w.recentEmissions {
		if !hexMatches(rec.BuildDescriptionHash, hashes) {
			continue
		}

		start := rec.group.Start()
		delta := sidecarAcceptedAt.Sub(start)
		if delta < 0 {
			delta = -delta
		}

		if !foundMatch || delta < bestDelta {
			best = rec
			bestDelta = delta
			foundMatch = true
		}
	}

	return best, foundMatch
}

// logStalledIfSuspicious emits a once-per-minute diagnostic when a scan found
// no manifests at all yet sidecars sit on disk — the exact state that was
// invisible before the project-local DD glob hint fix. Writes to Debug so a
// normal activate run stays quiet; set --debug to surface the signal.
func (w *Watcher) logStalledIfSuspicious(logger log.Logger, totalMatches, globCount int) {
	if totalMatches > 0 || w.SidecarReader == nil {
		return
	}

	pending := w.SidecarReader.PendingCount()
	if pending == 0 {
		return
	}

	now := w.now()
	if !w.lastStalledLogAt.IsZero() && now.Sub(w.lastStalledLogAt) < zeroMatchStallInterval {
		return
	}
	w.lastStalledLogAt = now

	ddPaths := w.SidecarReader.PendingDerivedDataPaths()
	logger.Debugf("enrichment watcher: 0 manifests across %d globs; %d uncorrelated sidecars pending (DD paths: %v)",
		globCount, pending, ddPaths)
}

// sidecarGlobs expands each pending DerivedDataPath into a
// LogStoreManifest.plist glob under its Logs tree. Returns nil when no
// SidecarReader is wired or no sidecar carries a DD path — HOME-relative
// defaults still cover IDE builds and the wrapper's managed DD root.
func (w *Watcher) sidecarGlobs() []string {
	if w.SidecarReader == nil {
		return nil
	}

	paths := w.SidecarReader.PendingDerivedDataPaths()
	if len(paths) == 0 {
		return nil
	}

	globs := make([]string, 0, len(paths))
	for _, p := range paths {
		globs = append(globs, filepath.Join(p, "Logs", "*", "LogStoreManifest.plist"))
	}

	return globs
}

func (w *Watcher) handleGroup(manifestPath string, group ManifestEntryGroup, seedOnly, synthetic bool) {
	logger := logOr(w.Logger)

	stop := group.Stop()
	// Age gate: HandledStore prunes seen-UUIDs after HandledManifestMaxAge, so a group older than that on-disk would otherwise be replayed as a fresh orphan on restart.
	if !stop.IsZero() && stop.Before(w.now().Add(-HandledManifestMaxAge)) {
		logger.Debugf("Watcher: skip stale group scheme=%s stop=%s uuids=%v", group.SchemeName(), stop.Format(time.RFC3339), group.UUIDs())

		return
	}

	if seedOnly {
		for _, uuid := range group.UUIDs() {
			w.seen[uuid] = struct{}{}
		}
		logger.Debugf("Watcher: seed-only mark scheme=%s uuids=%v", group.SchemeName(), group.UUIDs())

		return
	}

	if w.groupFullySeen(group) {
		logger.Debugf("Watcher: skip already-seen group scheme=%s uuids=%v", group.SchemeName(), group.UUIDs())

		return
	}

	key := groupKey(group)

	if synthetic || w.Handle == nil || w.MaxCorrelationRetries == 0 || !w.hasCorrelationSignal() {
		if w.Handle != nil {
			logger.Debugf("Watcher: handle-and-mark (no retry bucket) scheme=%s uuids=%v synthetic=%t", group.SchemeName(), group.UUIDs(), synthetic)
			w.Handle(manifestPath, group)
		}
		w.markGroupHandled(group)

		return
	}

	if _, pending := w.retries[key]; pending {
		switch {
		case w.canCorrelate(manifestPath, group):
			logger.Debugf("Watcher: pending match resolved scheme=%s attempts_left=%d uuids=%v", group.SchemeName(), w.retries[key], group.UUIDs())
			w.Handle(manifestPath, group)
			w.markGroupHandled(group)
			delete(w.retries, key)
		case w.retries[key] > 0:
			w.retries[key]--
			logger.Debugf("Watcher: pending still unmatched, decrement scheme=%s attempts_left=%d", group.SchemeName(), w.retries[key])
		default:
			logger.Debugf("Watcher: pending retries exhausted, minting orphan scheme=%s uuids=%v", group.SchemeName(), group.UUIDs())
			w.Handle(manifestPath, group)
			w.markGroupHandled(group)
			delete(w.retries, key)
		}

		return
	}

	// First-pass also consults the sidecar reader so a fast-flushing sidecar
	// (IDE flow: manifest lands, sidecar flushed on next idle beat) can
	// short-circuit the retry setup entirely.
	if w.canCorrelate(manifestPath, group) {
		logger.Debugf("Watcher: first-pass match scheme=%s uuids=%v", group.SchemeName(), group.UUIDs())
		w.Handle(manifestPath, group)
		w.markGroupHandled(group)

		return
	}

	w.retries[key] = w.MaxCorrelationRetries
	logger.Debugf("Watcher: unmatched, opening retry bucket scheme=%s attempts_left=%d", group.SchemeName(), w.MaxCorrelationRetries)
}

// hasCorrelationSignal is true when at least one of {MatchProbe, SidecarReader}
// could possibly answer "yes" to canCorrelate. Neither wired → the retry bucket
// has no signal to resolve on, so skip it and fire-and-mark immediately.
func (w *Watcher) hasCorrelationSignal() bool {
	return w.MatchProbe != nil || w.SidecarReader != nil
}

// canCorrelate: pending-record probe matches OR a sidecar overlapping the
// window is on disk. Side-effect free (Lookup consumes later). Supplies the
// DerivedData filter axis from manifestPath so parallel builds on different DD
// roots do not falsely satisfy each other's retry buckets. The Hashes axis is
// Enricher-only (buildDescriptionHash is sourced from metricsLookup, which
// only runs at emission time) so Has operates on DD alone.
func (w *Watcher) canCorrelate(manifestPath string, group ManifestEntryGroup) bool {
	if w.MatchProbe != nil && w.MatchProbe(group) {
		return true
	}
	if w.SidecarReader != nil && w.SidecarReader.Has(group, LookupFilter{DerivedData: paths.DerivedDataFromManifestPath(manifestPath)}) {
		return true
	}

	return false
}

// groupFullySeen gates on Primary().UUID so the gate agrees with the PUT-side
// identity (ManifestUUID → Primary().UUID). A late non-Primary entry joining
// the cluster (post-build indexing, preview compile) does NOT re-open the
// gate; a new Primary (new build action with higher command rank or earlier
// Start) does, because it changes the group's identity.
func (w *Watcher) groupFullySeen(group ManifestEntryGroup) bool {
	if len(group.Entries) == 0 {
		return false
	}

	_, ok := w.seen[group.Primary().UUID]

	return ok
}
