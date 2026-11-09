package wrapperless

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/fsnotify/fsnotify"
	"howett.net/plist"
)

type EndWatcher struct {
	HomeDir   string
	Logger    log.Logger
	OnEnd     func(End)
	NewSource func() (DirEventSource, error)
	Now       func() time.Time

	mu     sync.Mutex
	seen   map[string]struct{}
	src    DirEventSource
	cancel context.CancelFunc
	done   chan struct{}
}

func (w *EndWatcher) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.seen == nil {
		w.seen = make(map[string]struct{})
	}
	w.mu.Unlock()

	newSource := w.NewSource
	if newSource == nil {
		newSource = func() (DirEventSource, error) { return newFsnotifyAdapter() }
	}

	src, err := newSource()
	if err != nil {
		w.warn("end watcher source init: %s", err)

		return nil
	}

	for _, root := range w.derivedDataRoots() {
		w.addWatchesForRoot(src, root)
	}

	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	w.mu.Lock()
	w.src = src
	w.cancel = cancel
	w.done = done
	w.mu.Unlock()

	go w.loop(ctx, src, done)

	return nil
}

// MarkSeen records a uniqueIdentifier as already-emitted so a subsequent
// fsnotify tick does not re-fire for it. Used by restart-recovery when it
// synthesizes an End from the on-disk manifest before the watcher attaches.
func (w *EndWatcher) MarkSeen(uniqueID string) {
	if uniqueID == "" {
		return
	}

	w.mu.Lock()
	if w.seen == nil {
		w.seen = make(map[string]struct{})
	}
	w.seen[uniqueID] = struct{}{}
	w.mu.Unlock()
}

func (w *EndWatcher) Close() {
	w.mu.Lock()
	cancel := w.cancel
	src := w.src
	done := w.done
	w.cancel = nil
	w.src = nil
	w.done = nil
	w.mu.Unlock()

	if cancel == nil {
		return
	}

	cancel()

	if src != nil {
		_ = src.Close() //nolint:errcheck
	}

	if done != nil {
		<-done
	}
}

func (w *EndWatcher) derivedDataRoots() []string {
	home := w.HomeDir
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil || h == "" {
			return nil
		}
		home = h
	}

	matches, err := filepath.Glob(filepath.Join(home, "Library", "Developer", "Xcode", "DerivedData", "*"))
	if err != nil {
		return nil
	}

	return matches
}

func (w *EndWatcher) addWatchesForRoot(src DirEventSource, root string) {
	logsDir := filepath.Join(root, "Logs", buildDirName)
	if _, err := os.Stat(logsDir); err == nil {
		if addErr := src.Add(logsDir); addErr != nil {
			w.warn("end watcher add %s: %s", logsDir, addErr)
		}

		return
	}

	parent := filepath.Dir(logsDir)
	if _, err := os.Stat(parent); err != nil {
		return
	}

	if addErr := src.Add(parent); addErr != nil {
		w.warn("end watcher add %s: %s", parent, addErr)
	}
}

func (w *EndWatcher) loop(ctx context.Context, src DirEventSource, done chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			w.warn("end watcher goroutine panic: %v", r)
		}

		close(done)
	}()

	events := src.Events()
	errs := src.Errors()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}

			w.handleEvent(src, ev)
		case err, ok := <-errs:
			if !ok {
				return
			}

			if err != nil {
				w.warn("end watcher source error: %s", err)
			}
		}
	}
}

func (w *EndWatcher) handleEvent(src DirEventSource, ev fsnotify.Event) {
	if ev.Op&(fsnotify.Write|fsnotify.Create) == 0 {
		return
	}

	base := filepath.Base(ev.Name)
	if base == "LogStoreManifest.plist" {
		w.scanManifest(ev.Name)

		return
	}

	switch base {
	case "Logs", buildDirName:
		if addErr := src.Add(ev.Name); addErr != nil {
			w.warn("end watcher add %s: %s", ev.Name, addErr)
		}
	}
}

func (w *EndWatcher) scanManifest(path string) {
	entries, err := loadManifestEntries(path)
	if err != nil {
		w.warn("end watcher load %s: %s", path, err)

		return
	}

	for _, e := range entries {
		if !isSealedTerminal(e.HighLevelStatus) {
			continue
		}
		if e.TimeStopped == 0 || e.UniqueIdentifier == "" {
			continue
		}

		w.mu.Lock()
		if _, ok := w.seen[e.UniqueIdentifier]; ok {
			w.mu.Unlock()

			continue
		}
		w.seen[e.UniqueIdentifier] = struct{}{}
		w.mu.Unlock()

		if w.OnEnd == nil {
			continue
		}

		scheme := schemeFromSignature(e.Signature)
		success := e.HighLevelStatus != "E" && e.HighLevelStatus != "F"
		w.OnEnd(End{
			At:               w.now(),
			Scheme:           scheme,
			Success:          success,
			UniqueIdentifier: e.UniqueIdentifier,
			Duration:         cfAbsDuration(e.TimeStarted, e.TimeStopped),
		})
	}
}

// isSealedTerminal reports whether the highLevelStatus marks a completed log
// entry Xcode will not append to again. S=succeeded, F=failed, E=error.
func isSealedTerminal(status string) bool {
	return status == "S" || status == "F" || status == "E"
}

// schemeFromSignature strips the leading verb (Build / Test / Archive /
// Analyze) and returns the remainder. Xcode writes variants like "Build
// MyScheme project" or "Testing MySchemeTests"; the second token is the
// scheme.
func schemeFromSignature(sig string) string {
	s := strings.TrimSpace(sig)
	if s == "" {
		return ""
	}

	first := strings.SplitN(s, " ", 2)
	if len(first) < 2 {
		return ""
	}

	rest := first[1]
	for _, suffix := range []string{" project", " workspace"} {
		if idx := strings.Index(rest, suffix); idx >= 0 {
			rest = rest[:idx]
		}
	}

	return strings.TrimSpace(rest)
}

func cfAbsDuration(start, stop float64) time.Duration {
	if start == 0 || stop == 0 || stop <= start {
		return 0
	}

	return time.Duration((stop - start) * float64(time.Second))
}

type manifestLogEntry struct {
	UniqueIdentifier string  `plist:"uniqueIdentifier"`
	Signature        string  `plist:"signature"`
	HighLevelStatus  string  `plist:"highLevelStatus"`
	TimeStarted      float64 `plist:"timeStartedRecording"`
	TimeStopped      float64 `plist:"timeStoppedRecording"`
}

type manifestFile struct {
	Logs map[string]manifestLogEntry `plist:"logs"`
}

func loadManifestEntries(path string) ([]manifestLogEntry, error) {
	f, err := os.Open(path) //nolint:gosec // caller-supplied DD path
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close() //nolint:errcheck

	var raw manifestFile
	if err := plist.NewDecoder(f).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	out := make([]manifestLogEntry, 0, len(raw.Logs))
	for id, entry := range raw.Logs {
		if entry.UniqueIdentifier == "" {
			entry.UniqueIdentifier = id
		}
		out = append(out, entry)
	}

	return out, nil
}

func (w *EndWatcher) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}

	return time.Now()
}

func (w *EndWatcher) warn(format string, args ...any) {
	if w.Logger == nil {
		return
	}

	w.Logger.Warnf(format, args...)
}
