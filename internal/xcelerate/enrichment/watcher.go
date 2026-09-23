package enrichment

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

const DefaultPollInterval = 5 * time.Second

const DefaultDerivedDataGlob = "Library/Developer/Xcode/DerivedData/*/Logs/*/LogStoreManifest.plist"

const DefaultMaxCorrelationRetries = 6

type Watcher struct {
	HomeDir      string
	Globs        []string
	PollInterval time.Duration
	Handle       func(ManifestEntryGroup)
	Logger       log.Logger

	// MatchProbe returns true when a pending record for the group
	// exists and Handle would enrich under that record's InvocationID.
	// nil disables the retry bucket: groups fire immediately on first sight.
	MatchProbe            func(group ManifestEntryGroup) bool
	MaxCorrelationRetries int

	// TimeGap zero-value falls back to LocalGroupTimeGap.
	TimeGap time.Duration

	// HandledStore persists the seen-UUID set across restarts. nil disables
	// persistence (seen stays in-memory only) — this is the pre-persistence
	// behavior and is still what tests use unless they set the field.
	HandledStore *HandledManifestStore

	// Now is a test seam for the timestamp written into HandledStore records.
	// nil falls back to time.Now.
	Now func() time.Time

	seen    map[string]struct{}
	retries map[string]int
}

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

	if !w.seedSeenFromStore() {
		w.scan(true)
	}

	ticker := time.NewTicker(w.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.scan(false)
		}
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

	globs := w.Globs
	if len(globs) == 0 {
		globs = []string{DefaultDerivedDataGlob}
	}

	timeGap := w.TimeGap
	if timeGap == 0 {
		timeGap = LocalGroupTimeGap
	}

	for _, glob := range globs {
		matches, err := filepath.Glob(filepath.Join(w.HomeDir, glob))
		if err != nil {
			logger.Debugf("LogWatcher glob failed: %s", err)

			continue
		}

		for _, path := range matches {
			groups, err := LoadManifestGrouped(path, timeGap)
			if err != nil {
				logger.Debugf("LogWatcher failed to load %s: %s", path, err)

				continue
			}

			for _, group := range groups {
				w.handleGroup(group, seedOnly)
			}
		}
	}
}

func (w *Watcher) handleGroup(group ManifestEntryGroup, seedOnly bool) {
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

	if w.Handle == nil || w.MatchProbe == nil || w.MaxCorrelationRetries == 0 {
		if w.Handle != nil {
			logger.Debugf("Watcher: handle-and-mark (no retry bucket) scheme=%s uuids=%v", group.SchemeName(), group.UUIDs())
			w.Handle(group)
		}
		w.markGroupHandled(group)

		return
	}

	if _, pending := w.retries[key]; pending {
		switch {
		case w.MatchProbe(group):
			logger.Debugf("Watcher: pending match resolved scheme=%s attempts_left=%d uuids=%v", group.SchemeName(), w.retries[key], group.UUIDs())
			w.Handle(group)
			w.markGroupHandled(group)
			delete(w.retries, key)
		case w.retries[key] > 0:
			w.retries[key]--
			logger.Debugf("Watcher: pending still unmatched, decrement scheme=%s attempts_left=%d", group.SchemeName(), w.retries[key])
		default:
			logger.Debugf("Watcher: pending retries exhausted, minting orphan scheme=%s uuids=%v", group.SchemeName(), group.UUIDs())
			w.Handle(group)
			w.markGroupHandled(group)
			delete(w.retries, key)
		}

		return
	}

	if w.MatchProbe(group) {
		logger.Debugf("Watcher: first-pass match scheme=%s uuids=%v", group.SchemeName(), group.UUIDs())
		w.Handle(group)
		w.markGroupHandled(group)

		return
	}

	w.retries[key] = w.MaxCorrelationRetries
	logger.Debugf("Watcher: unmatched, opening retry bucket scheme=%s attempts_left=%d", group.SchemeName(), w.MaxCorrelationRetries)
}

// groupFullySeen returns false the moment a new UUID appears, so late-arriving
// entries force a re-emit of the enlarged aggregate instead of being dropped.
func (w *Watcher) groupFullySeen(group ManifestEntryGroup) bool {
	for _, uuid := range group.UUIDs() {
		if _, ok := w.seen[uuid]; !ok {
			return false
		}
	}

	return len(group.Entries) > 0
}
