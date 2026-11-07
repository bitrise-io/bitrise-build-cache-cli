package enrichment

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcactivitylog"
)

const DefaultPollInterval = 5 * time.Second

const DefaultDerivedDataGlob = "Library/Developer/Xcode/DerivedData/*/Logs/*/LogStoreManifest.plist"

type Watcher struct {
	HomeDir      string
	Globs        []string
	PollInterval time.Duration
	// Handle receives manifestPath so the callback can resolve the sibling
	// xcactivitylog. Each manifest entry is its own group; sub-build
	// attribution lives in scanOrphanSidecars via GroupSidecarsByProxyID.
	// handleGroup marks the group seen unconditionally after Handle.
	Handle func(manifestPath string, group ManifestEntryGroup)
	Logger log.Logger

	// HandledStore persists the seen-UUID set across restarts. nil disables
	// persistence (seen stays in-memory only) — this is the pre-persistence
	// behavior and is still what tests use unless they set the field.
	HandledStore *HandledManifestStore

	// CrashFallbackChannel feeds the PayloadStore's AfterFunc output back into
	// the scan loop so a timer fire does not race scanMu. nil disables draining;
	// the default-constructed Watcher behaves identically to the pre-ledger shape.
	CrashFallbackChannel <-chan string

	// CrashFallback re-emits the InvocationID that the fallback timer surfaced.
	// Wired to Enricher.EmitCrashFallback at construction. nil = log-only drain,
	// used by tests that only care about the channel read side.
	CrashFallback func(invocationID string)

	// SessionsDir is the on-disk sidecar directory the single-ID aggregator
	// reads through GroupSidecarsByProxyID. Empty disables the orphan-sidecar
	// PUT path.
	SessionsDir string

	// OrphanEmitter is called once per aggregated proxy-ID group the orphan
	// scan surfaces. Wired to Enricher.EmitOrphan in production. nil skips the
	// PUT path and leaves sidecars on disk.
	OrphanEmitter func(group *ProxyIDGroup)

	// SidecarReader drives the correlated manifest path: (a) Lookup finds the
	// sidecar peer for a manifest group so Enrich can key under its
	// WrapperSession.InvocationID; (b) PendingDerivedDataPaths turns each
	// unique DD path into a <DD>/Logs/*/LogStoreManifest.plist glob, layered
	// on top of the HOME-relative defaults so `xcodebuild -derivedDataPath`
	// still correlates; (c) PendingCount feeds the stalled-diagnostic log.
	// The uncorrelated orphan-sidecar PUT path reads sessions directly via
	// SessionsDir / OrphanEmitter.
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

	seen map[string]struct{}

	// tickEmittedIDs collects InvocationIDs the Enricher emitted via the
	// manifest path during the current scan tick so scanOrphanSidecars can
	// dedup. Scoped per tick: reset at scan start, cleared at scan tail.
	tickEmittedIDs map[string]struct{}

	// manifestEmittedIDs persists InvocationIDs the Enricher emitted via the
	// manifest path across the watcher's lifetime so a later orphan sweep
	// cannot overwrite a clean manifest PUT with an unclassified pseudo-group.
	// Scoped per-Watcher, not per-tick.
	manifestEmittedIDs map[string]struct{}

	// lastStalledLogAt rate-limits the "0 manifests + pending sidecars" debug
	// line to one entry per minute.
	lastStalledLogAt time.Time

	// scanMu serializes Run's ticker-driven scans with external one-shot calls
	// (ScanOnce, ScanOnceWithEmit) so background + flush-session paths cannot
	// race over seen / tickEmittedIDs.
	scanMu sync.Mutex

	// emitObserver is a transient callback installed by ScanOnceWithEmit that
	// receives each InvocationID emitted during that scan. Shared instance
	// means the flush-session RPC sees emissions off the one live Watcher
	// without spawning a parallel instance with divergent dedup state.
	emitObserver atomic.Pointer[func(string)]
}

// RecordEmittedInvocationID feeds the per-tick dedup set that
// scanOrphanSidecars reads: a sidecar whose ID just emitted via the manifest
// path must not re-emit as a pseudo-group. Payload-store ledger owns
// cross-tick dedup now; this helper exists only to populate tickEmittedIDs
// and fire the observer.
func (w *Watcher) RecordEmittedInvocationID(invocationID string) {
	if invocationID == "" {
		return
	}

	if w.tickEmittedIDs != nil {
		w.tickEmittedIDs[invocationID] = struct{}{}
	}

	if w.manifestEmittedIDs == nil {
		w.manifestEmittedIDs = make(map[string]struct{})
	}
	w.manifestEmittedIDs[invocationID] = struct{}{}

	if cb := w.emitObserver.Load(); cb != nil {
		(*cb)(invocationID)
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

func (w *Watcher) Run(ctx context.Context) {
	if w.PollInterval == 0 {
		w.PollInterval = DefaultPollInterval
	}

	w.scanMu.Lock()
	w.seen = make(map[string]struct{})
	seeded := w.seedSeenFromStore()
	if !seeded {
		w.scan(true)
	}
	w.scanMu.Unlock()

	ticker := time.NewTicker(w.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.scanMu.Lock()
			w.scan(false)
			w.scanMu.Unlock()
		}
	}
}

// ScanOnce runs a single synchronous pass; safe to call on a Watcher that was never Run.
// Used by the shutdown sweep: synthesizes pseudo-groups unconditionally (age gate dropped)
// so wrapperless invocations whose sidecars just closed still land in BE.
func (w *Watcher) ScanOnce() {
	w.scanMu.Lock()
	defer w.scanMu.Unlock()

	w.ensureMaps()
	w.scan(false)
}

// ScanOnceWithEmit runs one synchronous pass against the live Watcher state,
// routing each emitted InvocationID to onEmitted for the duration of the scan.
// Safe to call concurrently with Run — scanMu serializes.
func (w *Watcher) ScanOnceWithEmit(onEmitted func(string)) {
	w.scanMu.Lock()
	defer w.scanMu.Unlock()

	w.ensureMaps()

	if onEmitted != nil {
		w.emitObserver.Store(&onEmitted)
		defer w.emitObserver.Store(nil)
	}

	w.scan(false)
}

func (w *Watcher) ensureMaps() {
	if w.seen == nil {
		w.seen = make(map[string]struct{})
	}
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

func (w *Watcher) scan(seedOnly bool) {
	logger := logOr(w.Logger)

	w.tickEmittedIDs = make(map[string]struct{})
	defer func() {
		w.tickEmittedIDs = nil
	}()

	w.drainCrashFallbackChannel(logger)

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
		w.scanOrphanSidecars()
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

// scanOrphanSidecars reads the sessions dir with GroupSidecarsByProxyID and
// drives OrphanEmitter once per aggregated group whose InvocationID did NOT
// emit through the manifest path this tick. The classifier-derived Primary
// RowType rides into OrphanEmitter so the PUT's command label matches the
// row-type table in the single-ID ruleset.
//
// Sidecars left on disk at this point are ones the manifest-driven Enrich
// path did not consume via SidecarReader.Lookup (consumed sidecars are
// unlinked synchronously inside Handle before this function runs).
func (w *Watcher) scanOrphanSidecars() {
	if w.SessionsDir == "" || w.OrphanEmitter == nil {
		return
	}

	logger := logOr(w.Logger)

	groups := GroupSidecarsByProxyID(w.SessionsDir, logger)
	for invocationID, group := range groups {
		if _, emitted := w.tickEmittedIDs[invocationID]; emitted {
			logger.Debugf("Watcher: skip orphan for InvocationID %s (manifest path emitted this tick)", invocationID)

			continue
		}

		if _, emitted := w.manifestEmittedIDs[invocationID]; emitted {
			logger.Debugf("Watcher: skip orphan for InvocationID %s (manifest path emitted previously)", invocationID)
			unlinkSidecars(group.SidecarPaths, logger)

			continue
		}

		if group == nil || len(group.SidecarPaths) == 0 {
			continue
		}

		if !group.Terminal {
			logger.Debugf("Watcher: defer non-terminal orphan for InvocationID %s (build still running, %d sidecar(s))",
				invocationID, len(group.SidecarPaths))

			continue
		}

		if group.Blob == nil || group.Blob.IsEmpty() {
			logger.Debugf("Watcher: suppress zero-blob orphan for InvocationID %s (no CAS transfer, %d sidecar(s))",
				invocationID, len(group.SidecarPaths))
			unlinkSidecars(group.SidecarPaths, logger)

			continue
		}

		w.OrphanEmitter(group)
	}
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

	if w.Handle != nil {
		logger.Debugf("Watcher: handle-and-mark scheme=%s uuids=%v synthetic=%t", group.SchemeName(), group.UUIDs(), synthetic)
		w.Handle(manifestPath, group)
	}
	w.markGroupHandled(group)
}

// drainCrashFallbackChannel non-blockingly reads every ripe InvocationID the
// PayloadStore's crash-fallback AfterFunc queued since the last scan and
// re-emits via CrashFallback (Enricher.EmitCrashFallback in production).
// Runs under scanMu so the re-emit cannot race scanOrphanSidecars for the
// same InvocationID.
func (w *Watcher) drainCrashFallbackChannel(logger log.Logger) {
	if w.CrashFallbackChannel == nil {
		return
	}

	for {
		select {
		case id := <-w.CrashFallbackChannel:
			logger.Debugf("payload crash-fallback fired for %s (no terminal signal in window)", id)
			if w.CrashFallback != nil {
				w.CrashFallback(id)
			}
		default:
			return
		}
	}
}

// groupFullySeen returns true only when every UUID in the group is already in
// w.seen. Defensive all-UUIDs-seen check — groups are always one entry today
// but the gate survives if grouping gains multi-entry shapes again.
func (w *Watcher) groupFullySeen(group ManifestEntryGroup) bool {
	if len(group.Entries) == 0 {
		return false
	}

	for _, uuid := range group.UUIDs() {
		if _, ok := w.seen[uuid]; !ok {
			return false
		}
	}

	return true
}
