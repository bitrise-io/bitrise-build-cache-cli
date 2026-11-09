package wrapperless

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/fsnotify/fsnotify"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildrequest"
)

const (
	xcbuilddataSuffix = ".xcbuilddata"
	buildDirName      = "Build"
)

type DirEventSource interface {
	Events() <-chan fsnotify.Event
	Errors() <-chan error
	Add(path string) error
	Close() error
}

type fsnotifyAdapter struct {
	w *fsnotify.Watcher
}

func newFsnotifyAdapter() (*fsnotifyAdapter, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("new fsnotify watcher: %w", err)
	}

	return &fsnotifyAdapter{w: w}, nil
}

func (a *fsnotifyAdapter) Events() <-chan fsnotify.Event { return a.w.Events }
func (a *fsnotifyAdapter) Errors() <-chan error          { return a.w.Errors }
func (a *fsnotifyAdapter) Add(path string) error {
	//nolint:wrapcheck
	return a.w.Add(path)
}

func (a *fsnotifyAdapter) Close() error {
	//nolint:wrapcheck
	return a.w.Close()
}

type StartWatcher struct {
	HomeDir   string
	Logger    log.Logger
	OnStart   func(Start)
	NewSource func() (DirEventSource, error)
	Now       func() time.Time

	mu     sync.Mutex
	src    DirEventSource
	cancel context.CancelFunc
	done   chan struct{}
}

func (w *StartWatcher) Start(ctx context.Context) error {
	newSource := w.NewSource
	if newSource == nil {
		newSource = func() (DirEventSource, error) { return newFsnotifyAdapter() }
	}

	src, err := newSource()
	if err != nil {
		w.warn("start watcher source init: %s", err)

		return nil
	}

	roots := w.derivedDataRoots()
	for _, root := range roots {
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

func (w *StartWatcher) Close() {
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

func (w *StartWatcher) derivedDataRoots() []string {
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

func (w *StartWatcher) addWatchesForRoot(src DirEventSource, root string) {
	for _, dir := range xcbuildDataDirsFor(root) {
		if _, err := os.Stat(dir); err == nil {
			if addErr := src.Add(dir); addErr != nil {
				w.warn("start watcher add %s: %s", dir, addErr)
			}

			continue
		}

		grandparent := filepath.Dir(filepath.Dir(dir))
		if _, err := os.Stat(grandparent); err != nil {
			continue
		}

		if addErr := src.Add(grandparent); addErr != nil {
			w.warn("start watcher add %s: %s", grandparent, addErr)
		}
	}
}

func xcbuildDataDirsFor(root string) []string {
	return []string{
		filepath.Join(root, buildDirName, "Intermediates.noindex", "XCBuildData"),
		filepath.Join(root, "Index.noindex", buildDirName, "Intermediates.noindex", "XCBuildData"),
	}
}

func isXcbuildDataAncestorDir(base string) bool {
	switch base {
	case buildDirName, "Index.noindex", "Intermediates.noindex", "XCBuildData":
		return true
	}

	return false
}

func (w *StartWatcher) loop(ctx context.Context, src DirEventSource, done chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			w.warn("start watcher goroutine panic: %v", r)
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
				w.warn("start watcher source error: %s", err)
			}
		}
	}
}

func (w *StartWatcher) handleEvent(src DirEventSource, ev fsnotify.Event) {
	if ev.Op&fsnotify.Create == 0 {
		return
	}

	base := filepath.Base(ev.Name)
	if strings.HasSuffix(base, xcbuilddataSuffix) {
		w.emitStart(ev.Name)

		return
	}

	if isXcbuildDataAncestorDir(base) {
		if addErr := src.Add(ev.Name); addErr != nil {
			w.warn("start watcher add %s: %s", ev.Name, addErr)
		}
	}
}

func (w *StartWatcher) emitStart(xcbuildDataDir string) {
	hex := strings.TrimSuffix(filepath.Base(xcbuildDataDir), xcbuilddataSuffix)
	if hex == "" {
		return
	}

	root := rootForXCBuildDataPath(xcbuildDataDir)

	info, err := buildrequest.Load(root, hex)
	if err != nil {
		if !errors.Is(err, buildrequest.ErrNotFound) {
			w.warn("start watcher load build-request %s: %s", xcbuildDataDir, err)
		}

		return
	}

	action, ok := classifyPrimary(info)
	if !ok {
		return
	}

	if w.OnStart == nil {
		return
	}

	w.OnStart(Start{
		At:              w.now(),
		DerivedDataPath: root,
		BuildRequestHex: hex,
		Action:          action,
	})
}

// classifyPrimary returns the PrimaryAction when the build-request represents
// a user-facing build (qos absent), archive, test, or analyze.
func classifyPrimary(info buildrequest.Info) (PrimaryAction, bool) {
	switch info.Action {
	case "build":
		if info.QoS == "" {
			return PrimaryActionBuild, true
		}
	case "archive":
		return PrimaryActionArchive, true
	case "test":
		return PrimaryActionTest, true
	case "analyze":
		return PrimaryActionAnalyze, true
	}

	return "", false
}

func rootForXCBuildDataPath(xcbuildDataDir string) string {
	dir := filepath.Dir(xcbuildDataDir)
	dir = filepath.Dir(dir)
	dir = filepath.Dir(dir)
	buildOrIndex := filepath.Base(dir)
	parent := filepath.Dir(dir)
	if buildOrIndex == buildDirName && filepath.Base(parent) == "Index.noindex" {
		return filepath.Dir(parent)
	}

	return parent
}

func (w *StartWatcher) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}

	return time.Now()
}

func (w *StartWatcher) warn(format string, args ...any) {
	if w.Logger == nil {
		return
	}

	w.Logger.Warnf(format, args...)
}
