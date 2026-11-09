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
		logger := w.Logger
		newSource = func() (DirEventSource, error) { return newNotifyAdapter(logger), nil }
	}

	src, err := newSource()
	if err != nil {
		w.warn("end watcher source init: %s", err)

		return nil
	}

	w.infof("EndWatcher: source init ok")

	ddRoot := w.derivedDataDir()
	w.infof("EndWatcher: dd root = %q", ddRoot)

	if ddRoot != "" {
		w.installWatch(src, ddRoot)
	}

	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	w.mu.Lock()
	w.src = src
	w.cancel = cancel
	w.done = done
	w.mu.Unlock()

	// Seed-walk the DD tree for any already-sealed LogStoreManifest entries
	// so a proxy that started after a build finished still picks them up.
	seeded := 0
	if ddRoot != "" {
		seeded = w.seedWalk(ddRoot)
	}
	w.infof("EndWatcher: seed walk done (%d sealed found)", seeded)

	go w.loop(ctx, src, done)
	w.infof("EndWatcher: loop goroutine started")

	return nil
}

func (w *EndWatcher) installWatch(src DirEventSource, ddRoot string) {
	if mkErr := os.MkdirAll(ddRoot, 0o755); mkErr != nil {
		w.warn("end watcher mkdir %s: %s", ddRoot, mkErr)

		return
	}

	w.infof("EndWatcher: mkdir ok")

	path := recursivePath(ddRoot)
	if addErr := src.Watch(path, EventCreate, EventWrite); addErr != nil {
		w.warn("end watcher watch %s: %s", ddRoot, addErr)

		return
	}

	w.infof("EndWatcher: watching %s recursively", path)
}

// MarkSeen records a uniqueIdentifier as already-emitted so a subsequent
// event tick does not re-fire for it. Used by restart-recovery when it
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
		src.Stop()
	}

	if done != nil {
		<-done
	}
}

// derivedDataDir mirrors StartWatcher.derivedDataDir: the recursive FSEvents
// watch is installed on the DD root so manifest writes under any app subdir
// (including those created after the watcher starts) are observed.
func (w *EndWatcher) derivedDataDir() string {
	home := w.HomeDir
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil || h == "" {
			return ""
		}
		home = h
	}

	return filepath.Join(home, "Library", "Developer", "Xcode", "DerivedData")
}

// seedWalk scans every existing LogStoreManifest.plist at Start, in case the
// proxy started after Xcode already sealed an entry. Returns the number of
// manifests found on disk (not the number of sealed entries emitted — those go
// through scanManifest's own dedup / seal gate).
func (w *EndWatcher) seedWalk(ddRoot string) int {
	entries, err := os.ReadDir(ddRoot)
	if err != nil {
		return 0
	}

	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		path := filepath.Join(ddRoot, e.Name(), "Logs", buildDirName, "LogStoreManifest.plist")
		if _, statErr := os.Stat(path); statErr == nil {
			count++
			w.scanManifest(path)
		}
	}

	return count
}

func (w *EndWatcher) loop(ctx context.Context, src DirEventSource, done chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			w.warn("end watcher goroutine panic: %v", r)
		}

		close(done)
	}()

	w.infof("EndWatcher: loop entered")

	events := src.Events()
	eventCount := 0

	for {
		select {
		case <-ctx.Done():
			w.infof("EndWatcher: ctx done, exiting")

			return
		case ev, ok := <-events:
			if !ok {
				w.infof("EndWatcher: event chan closed, exiting")

				return
			}

			eventCount++
			w.infof("EndWatcher: event #%d op=%d %s", eventCount, ev.Op, ev.Path)
			w.handleEvent(ev)
		}
	}
}

func (w *EndWatcher) handleEvent(ev Event) {
	w.infof("EndWatcher: handleEvent %s", ev.Path)

	if ev.Op&(EventWrite|EventCreate) == 0 {
		w.infof("EndWatcher: skip %s reason=op (op=%d)", ev.Path, ev.Op)

		return
	}

	// Xcode atomic-replaces LogStoreManifest.plist: a temp sibling
	// (LogStoreManifest.plist.sb-<hex>) is renamed into place. The rename
	// surfaces as a Write/Create on the final name — we filter to that name
	// only so the sb- temp variants (which also pass the op filter) are
	// dropped here with a reason.
	if filepath.Base(ev.Path) != "LogStoreManifest.plist" {
		w.infof("EndWatcher: skip %s reason=name", ev.Path)

		return
	}

	w.scanManifest(ev.Path)
}

func (w *EndWatcher) scanManifest(path string) {
	entries, err := loadManifestEntries(path)
	if err != nil {
		w.warn("end watcher load %s: %s", path, err)
		w.infof("EndWatcher: skip %s reason=load-failed", path)

		return
	}

	for _, e := range entries {
		if !isSealedTerminal(e.HighLevelStatus) {
			w.infof("EndWatcher: skip %s reason=not-sealed uuid=%s status=%q", path, e.UniqueIdentifier, e.HighLevelStatus)

			continue
		}
		if e.TimeStopped == 0 || e.UniqueIdentifier == "" {
			w.infof("EndWatcher: skip %s reason=missing-fields uuid=%s timeStopped=%v", path, e.UniqueIdentifier, e.TimeStopped)

			continue
		}

		w.mu.Lock()
		if _, ok := w.seen[e.UniqueIdentifier]; ok {
			w.mu.Unlock()
			w.infof("EndWatcher: skip %s reason=already-seen uuid=%s", path, e.UniqueIdentifier)

			continue
		}
		w.seen[e.UniqueIdentifier] = struct{}{}
		w.mu.Unlock()

		if w.OnEnd == nil {
			w.infof("EndWatcher: skip %s reason=no-OnEnd uuid=%s", path, e.UniqueIdentifier)

			continue
		}

		scheme := schemeFromSignature(e.Signature)
		success := e.HighLevelStatus != "E" && e.HighLevelStatus != "F"
		w.infof("EndWatcher: emit uuid=%s scheme=%s status=%s", e.UniqueIdentifier, scheme, e.HighLevelStatus)
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

func (w *EndWatcher) infof(format string, args ...any) {
	if w.Logger == nil {
		return
	}

	w.Logger.Infof(format, args...)
}
