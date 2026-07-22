package xcode_app_casinject

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Logger keeps the package free of the project's logger interface so we can
// unit-test with a simple stub.
type Logger interface {
	Infof(format string, args ...any)
	Debugf(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

type WatchParams struct {
	// Roots to watch recursively. Typically DerivedData/<Project>*.
	Roots []string
	// SocketPath to inject as RemoteService.Path.
	SocketPath string
	// Logger receives lifecycle + per-file logs.
	Logger Logger
	// PollInterval scans Roots for new subdirectories to add watches to.
	// fsnotify does not recurse; new Intermediates.noindex/... paths get
	// caught by both directory-create events AND the poll fallback.
	PollInterval time.Duration
}

type Watcher struct {
	params WatchParams
	fsw    *fsnotify.Watcher

	mu      sync.Mutex
	watched map[string]struct{}

	stats stats
}

type stats struct {
	discovered int
	injected   int
	skipped    int
	errored    int
}

func NewWatcher(p WatchParams) (*Watcher, error) {
	if len(p.Roots) == 0 {
		return nil, errors.New("no roots to watch")
	}
	if p.SocketPath == "" {
		return nil, errors.New("empty socket path")
	}
	if p.PollInterval <= 0 {
		p.PollInterval = 2 * time.Second
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("new fsnotify watcher: %w", err)
	}

	return &Watcher{
		params:  p,
		fsw:     fsw,
		watched: map[string]struct{}{},
	}, nil
}

// Run drives the injection loop until ctx is cancelled.
//
// Bootstrap: before entering the select, every root is walked and any existing
// directory is registered with fsnotify while any existing .cas-config is
// injected immediately. This closes the race where DerivedData already
// contains a config by the time the watcher starts.
//
// Self-echo idempotency: rewriting a .cas-config produces a Write event that
// re-enters handleEvent. InjectFile compares the desired RemoteService.Path
// against the file's current value and returns changed=false when they match,
// so the second pass is a stat + read + compare with no filesystem write. New
// events therefore stop propagating after the first successful injection.
//
// Recursive add semantics: fsnotify on macOS (FSEvents) does not recurse into
// subdirectories created after Add. Two mechanisms compensate: directory
// Create events call addRecursive on the new subtree, and the poll ticker
// re-walks every root so late-arriving trees (SourcePackages, XCBuildData,
// Intermediates.noindex/...) are eventually picked up even if the Create was
// missed.
func (w *Watcher) Run(ctx context.Context) error {
	defer w.fsw.Close()

	for _, root := range w.params.Roots {
		w.addRecursive(root)
	}

	ticker := time.NewTicker(w.params.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.params.Logger.Infof("watcher stopping: discovered=%d injected=%d skipped=%d errored=%d",
				w.stats.discovered, w.stats.injected, w.stats.skipped, w.stats.errored)

			return nil

		case ev, ok := <-w.fsw.Events:
			if !ok {
				return errors.New("fsnotify events channel closed")
			}
			w.handleEvent(ev)

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return errors.New("fsnotify errors channel closed")
			}
			w.params.Logger.Warnf("fsnotify error: %v", err)

		case <-ticker.C:
			for _, root := range w.params.Roots {
				w.addRecursive(root)
			}
		}
	}
}

func (w *Watcher) handleEvent(ev fsnotify.Event) {
	w.params.Logger.Debugf("fsnotify event: op=%s path=%s", ev.Op.String(), ev.Name)

	// fsnotify bundles bits: a single event may carry Create|Write. Handle
	// them independently instead of with a switch so both fire when needed.
	if ev.Op.Has(fsnotify.Create) {
		info, err := os.Lstat(ev.Name)
		if err == nil {
			if info.IsDir() {
				w.addRecursive(ev.Name)
			} else if IsCasConfigPath(ev.Name) {
				w.tryInject(ev.Name)
			}
		}
	}

	if ev.Op.Has(fsnotify.Write) && IsCasConfigPath(ev.Name) {
		w.tryInject(ev.Name)
	}
}

func (w *Watcher) tryInject(path string) {
	w.stats.discovered++

	changed, err := InjectFile(path, w.params.SocketPath)
	if err != nil {
		w.stats.errored++
		w.params.Logger.Errorf("inject %s: %v", path, err)

		return
	}
	if !changed {
		w.stats.skipped++
		w.params.Logger.Debugf("skip (already patched): %s", path)

		return
	}

	w.stats.injected++

	now := time.Now()
	if info, statErr := os.Stat(path); statErr == nil {
		w.params.Logger.Infof("injected RemoteService into %s (modtime=%s injected_at=%s)",
			path, info.ModTime().Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	} else {
		w.params.Logger.Infof("injected RemoteService into %s (injected_at=%s; stat failed: %v)",
			path, now.Format(time.RFC3339Nano), statErr)
	}
}

func (w *Watcher) addRecursive(root string) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}

			return nil
		}
		if !d.IsDir() {
			if IsCasConfigPath(p) {
				w.tryInject(p)
			}

			return nil
		}
		// Skip well-known noisy DerivedData subtrees that never contain
		// .cas-config. Each entry saves both an fsnotify Add and a
		// recurring poll-scan traversal.
		switch filepath.Base(p) {
		case "ModuleCache.noindex",
			"SDKStatCaches.noindex",
			"Index.noindex",
			"Logs",
			"SourcePackages",
			"XCBuildData":
			return filepath.SkipDir
		}
		w.addDir(p)

		return nil
	})
}

func (w *Watcher) addDir(dir string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, ok := w.watched[dir]; ok {
		return
	}
	if err := w.fsw.Add(dir); err != nil {
		w.params.Logger.Debugf("watch add %s: %v", dir, err)

		return
	}
	w.watched[dir] = struct{}{}
	w.params.Logger.Debugf("watching %s", dir)
}
