package enrichment

import (
	"context"
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
	Handle       func(ManifestEntry)
	Logger       log.Logger

	// MatchProbe returns true when a pending record for the entry
	// exists and Handle would enrich under that record's InvocationID.
	// nil disables the retry bucket: entries fire immediately on first sight.
	MatchProbe            func(entry ManifestEntry) bool
	MaxCorrelationRetries int

	// OrphanMintDeadline gates the orphan-mint branch: while the entry's
	// Stop is within the deadline the retry bucket is kept alive regardless
	// of MaxCorrelationRetries. Zero preserves the pre-existing count-only
	// behavior. MaxCorrelationRetries stays as a hard upper bound.
	OrphanMintDeadline time.Duration

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

// deadlineElapsed reports whether entry.Stop is past OrphanMintDeadline.
func (w *Watcher) deadlineElapsed(entry ManifestEntry) bool {
	if w.OrphanMintDeadline <= 0 || entry.Stop.IsZero() {
		return false
	}

	return w.now().Sub(entry.Stop) > w.OrphanMintDeadline
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
	globs := w.Globs
	if len(globs) == 0 {
		globs = []string{DefaultDerivedDataGlob}
	}

	WalkManifests(w.HomeDir, globs, w.Logger, func(_ string, entries []ManifestEntry) {
		for _, entry := range entries {
			w.handleEntry(entry, seedOnly)
		}
	})
}

func (w *Watcher) handleEntry(entry ManifestEntry, seedOnly bool) {
	logger := logOr(w.Logger)

	// Age gate: HandledStore prunes seen-UUIDs after HandledManifestMaxAge, so an entry older than that on-disk would otherwise be replayed as a fresh orphan on restart.
	if !entry.Stop.IsZero() && entry.Stop.Before(w.now().Add(-HandledManifestMaxAge)) {
		logger.Debugf("Watcher: skip stale entry uuid=%s stop=%s", entry.UUID, entry.Stop.Format(time.RFC3339))

		return
	}

	if seedOnly {
		w.seen[entry.UUID] = struct{}{}
		logger.Debugf("Watcher: seed-only mark uuid=%s scheme=%s", entry.UUID, entry.SchemeName)

		return
	}

	if _, ok := w.seen[entry.UUID]; ok {
		logger.Debugf("Watcher: skip already-seen uuid=%s", entry.UUID)

		return
	}

	if w.Handle == nil || w.MatchProbe == nil || w.MaxCorrelationRetries == 0 {
		if w.Handle != nil {
			logger.Debugf("Watcher: handle-and-mark (no retry bucket) uuid=%s scheme=%s", entry.UUID, entry.SchemeName)
			w.Handle(entry)
		}
		w.markHandled(entry.UUID)

		return
	}

	if _, pending := w.retries[entry.UUID]; pending {
		switch {
		case w.MatchProbe(entry):
			logger.Debugf("Watcher: pending match resolved uuid=%s attempts_left=%d scheme=%s", entry.UUID, w.retries[entry.UUID], entry.SchemeName)
			w.Handle(entry)
			w.markHandled(entry.UUID)
			delete(w.retries, entry.UUID)
		case w.deadlineElapsed(entry):
			waited := w.now().Sub(entry.Stop).Round(time.Second)
			logger.Infof("Watcher: orphan mint uuid=%s scheme=%s waited=%s", entry.UUID, entry.SchemeName, waited)
			w.Handle(entry)
			w.markHandled(entry.UUID)
			delete(w.retries, entry.UUID)
		case w.OrphanMintDeadline > 0 && !entry.Stop.IsZero():
			logger.Debugf("Watcher: pending unmatched, hold for deadline uuid=%s", entry.UUID)
		case w.retries[entry.UUID] > 0:
			w.retries[entry.UUID]--
			logger.Debugf("Watcher: pending still unmatched, decrement uuid=%s attempts_left=%d", entry.UUID, w.retries[entry.UUID])
		default:
			waited := w.now().Sub(entry.Stop).Round(time.Second)
			logger.Infof("Watcher: orphan mint uuid=%s scheme=%s waited=%s", entry.UUID, entry.SchemeName, waited)
			w.Handle(entry)
			w.markHandled(entry.UUID)
			delete(w.retries, entry.UUID)
		}

		return
	}

	if w.MatchProbe(entry) {
		logger.Debugf("Watcher: first-pass match uuid=%s scheme=%s", entry.UUID, entry.SchemeName)
		w.Handle(entry)
		w.markHandled(entry.UUID)

		return
	}

	w.retries[entry.UUID] = w.MaxCorrelationRetries
	logger.Debugf("Watcher: unmatched, opening retry bucket uuid=%s attempts_left=%d", entry.UUID, w.MaxCorrelationRetries)
}
