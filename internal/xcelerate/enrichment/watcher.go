package enrichment

import (
	"context"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

const DefaultPollInterval = 5 * time.Second

const DefaultDerivedDataGlob = "Library/Developer/Xcode/DerivedData/*/Logs/Build/LogStoreManifest.plist"

type Watcher struct {
	HomeDir      string
	Glob         string
	PollInterval time.Duration
	Handle       func(ManifestEntry)
	Logger       log.Logger

	seen map[string]struct{}
}

// First scan seeds seen without emitting so restart doesn't replay historic builds.
func (w *Watcher) Run(ctx context.Context) {
	if w.PollInterval == 0 {
		w.PollInterval = DefaultPollInterval
	}

	if w.Glob == "" {
		w.Glob = DefaultDerivedDataGlob
	}

	w.seen = make(map[string]struct{})

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
	logger := w.Logger

	matches, err := filepath.Glob(filepath.Join(w.HomeDir, w.Glob))
	if err != nil {
		if logger != nil {
			logger.Warnf("LogWatcher glob failed: %s", err)
		}

		return
	}

	for _, path := range matches {
		entries, err := LoadManifest(path)
		if err != nil {
			if logger != nil {
				logger.Warnf("LogWatcher failed to load %s: %s", path, err)
			}

			continue
		}

		for _, entry := range entries {
			if _, ok := w.seen[entry.UUID]; ok {
				continue
			}

			w.seen[entry.UUID] = struct{}{}

			if seedOnly || w.Handle == nil {
				continue
			}

			w.Handle(entry)
		}
	}
}
