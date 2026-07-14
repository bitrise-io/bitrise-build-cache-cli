package enrichment

import (
	"context"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

const DefaultPollInterval = 5 * time.Second

const DefaultDerivedDataGlob = "Library/Developer/Xcode/DerivedData/*/Logs/Build/LogStoreManifest.plist"

const DefaultMaxCorrelationRetries = 6

type Watcher struct {
	HomeDir      string
	Glob         string
	PollInterval time.Duration
	Handle       func(ManifestEntry)
	Logger       log.Logger

	// MatchProbe returns true when a pending record for the entry
	// exists and Handle would enrich under that record's InvocationID.
	// nil disables the retry bucket: entries fire immediately on first sight.
	MatchProbe            func(entry ManifestEntry) bool
	MaxCorrelationRetries int

	seen    map[string]struct{}
	retries map[string]int
}

// First scan seeds seen without emitting so restart doesn't replay historic builds.
func (w *Watcher) Run(ctx context.Context) {
	if w.PollInterval == 0 {
		w.PollInterval = DefaultPollInterval
	}

	w.seen = make(map[string]struct{})
	w.retries = make(map[string]int)

	w.scan(true)

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

func (w *Watcher) scan(seedOnly bool) {
	logger := logOr(w.Logger)

	glob := w.Glob
	if glob == "" {
		glob = DefaultDerivedDataGlob
	}

	matches, err := filepath.Glob(filepath.Join(w.HomeDir, glob))
	if err != nil {
		logger.Warnf("LogWatcher glob failed: %s", err)

		return
	}

	for _, path := range matches {
		entries, err := LoadManifest(path)
		if err != nil {
			logger.Warnf("LogWatcher failed to load %s: %s", path, err)

			continue
		}

		for _, entry := range entries {
			w.handleEntry(entry, seedOnly)
		}
	}
}

func (w *Watcher) handleEntry(entry ManifestEntry, seedOnly bool) {
	if seedOnly {
		w.seen[entry.UUID] = struct{}{}

		return
	}

	if _, ok := w.seen[entry.UUID]; ok {
		return
	}

	// Fast path preserves existing behavior when caller doesn't opt into correlation-aware retry.
	if w.Handle == nil || w.MatchProbe == nil || w.MaxCorrelationRetries == 0 {
		w.seen[entry.UUID] = struct{}{}

		if w.Handle != nil {
			w.Handle(entry)
		}

		return
	}

	if _, pending := w.retries[entry.UUID]; pending {
		switch {
		case w.MatchProbe(entry):
			w.Handle(entry)
			w.seen[entry.UUID] = struct{}{}
			delete(w.retries, entry.UUID)
		case w.retries[entry.UUID] > 1:
			w.retries[entry.UUID]--
		default:
			// Last chance exhausted: fire so Enricher can mint an orphan invocation.
			w.Handle(entry)
			w.seen[entry.UUID] = struct{}{}
			delete(w.retries, entry.UUID)
		}

		return
	}

	if w.MatchProbe(entry) {
		w.Handle(entry)
		w.seen[entry.UUID] = struct{}{}

		return
	}

	w.retries[entry.UUID] = w.MaxCorrelationRetries
}
