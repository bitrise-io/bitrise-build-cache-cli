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
	"github.com/rjeczalik/notify"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildrequest"
)

const (
	xcbuilddataSuffix = ".xcbuilddata"
	buildDirName      = "Build"
	eventChanBuffer   = 64
)

// EventOp is the subset of notify events this package cares about.
type EventOp uint32

const (
	EventCreate EventOp = 1 << iota
	EventWrite
)

// Event is the package-local representation of a filesystem event, decoupled
// from notify.EventInfo so tests can drive the watchers synchronously.
type Event struct {
	Path string
	Op   EventOp
}

// DirEventSource is a thin seam over the notify API. Production uses the
// notify-backed implementation; tests supply a fake source that pushes
// Events directly into the channel.
type DirEventSource interface {
	Events() <-chan Event
	Watch(root string, ops ...EventOp) error
	Stop()
}

type notifyAdapter struct {
	events  chan Event
	raw     chan notify.EventInfo
	stopMu  sync.Mutex
	stopped bool
}

func newNotifyAdapter() *notifyAdapter {
	raw := make(chan notify.EventInfo, eventChanBuffer)
	out := make(chan Event, eventChanBuffer)
	a := &notifyAdapter{events: out, raw: raw}

	go a.pump()

	return a
}

func (a *notifyAdapter) pump() {
	for ev := range a.raw {
		op := EventOp(0)
		if ev.Event()&notify.Create != 0 {
			op |= EventCreate
		}
		if ev.Event()&notify.Write != 0 {
			op |= EventWrite
		}
		if op == 0 {
			continue
		}

		select {
		case a.events <- Event{Path: ev.Path(), Op: op}:
		default:
			// Buffer full — drop rather than block the notify dispatcher.
			// The seed walk at Start time guarantees we still pick up
			// anything dropped here on the next relevant trigger.
		}
	}

	close(a.events)
}

func (a *notifyAdapter) Events() <-chan Event { return a.events }

func (a *notifyAdapter) Watch(root string, ops ...EventOp) error {
	ev := notify.Event(0)
	for _, op := range ops {
		if op&EventCreate != 0 {
			ev |= notify.Create
		}
		if op&EventWrite != 0 {
			ev |= notify.Write
		}
	}
	if ev == 0 {
		return fmt.Errorf("notify watch %s: no event ops", root)
	}

	if err := notify.Watch(root, a.raw, ev); err != nil {
		return fmt.Errorf("notify watch %s: %w", root, err)
	}

	return nil
}

func (a *notifyAdapter) Stop() {
	a.stopMu.Lock()
	if a.stopped {
		a.stopMu.Unlock()

		return
	}
	a.stopped = true
	a.stopMu.Unlock()

	notify.Stop(a.raw)
	close(a.raw)
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

	seenMu      sync.Mutex
	seenHexDirs map[string]struct{}
}

func (w *StartWatcher) Start(ctx context.Context) error {
	newSource := w.NewSource
	if newSource == nil {
		newSource = func() (DirEventSource, error) { return newNotifyAdapter(), nil }
	}

	src, err := newSource()
	if err != nil {
		w.warn("start watcher source init: %s", err)

		return nil
	}

	ddRoot := w.derivedDataDir()
	if ddRoot != "" {
		if mkErr := os.MkdirAll(ddRoot, 0o755); mkErr != nil {
			w.warn("start watcher mkdir %s: %s", ddRoot, mkErr)
		} else if addErr := src.Watch(recursivePath(ddRoot), EventCreate); addErr != nil {
			w.warn("start watcher watch %s: %s", ddRoot, addErr)
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	w.mu.Lock()
	w.src = src
	w.cancel = cancel
	w.done = done
	w.mu.Unlock()

	// Seed-walk the DD tree for any xcbuilddata that already exists when
	// the watcher starts. Closes the window where Xcode created dirs
	// before the recursive watch was installed.
	if ddRoot != "" {
		w.seedWalk(ddRoot)
	}

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
		src.Stop()
	}

	if done != nil {
		<-done
	}
}

// derivedDataDir returns the parent DerivedData dir so we can install a
// single recursive FSEvents watch that captures every xcbuilddata CREATE
// below it — including dirs that appear after a wipe or a fresh Xcode
// session, without any per-ancestor stair-step add.
func (w *StartWatcher) derivedDataDir() string {
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

// recursivePath is rjeczalik/notify's convention for a recursive watchpoint.
func recursivePath(dir string) string {
	return filepath.Join(dir, "...")
}

// seedWalk scans the DD subtree for pre-existing xcbuilddata dirs so a proxy
// that started after Xcode still catches in-flight builds. Idempotent with
// the notify stream via the seen-set.
func (w *StartWatcher) seedWalk(ddRoot string) {
	_ = filepath.WalkDir(ddRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			//nolint:nilerr // skip the un-readable subtree, keep walking siblings
			return filepath.SkipDir
		}
		if !d.IsDir() {
			return nil
		}

		if strings.HasSuffix(d.Name(), xcbuilddataSuffix) {
			w.emitStartOnce(path)

			return filepath.SkipDir
		}

		return nil
	})
}

func (w *StartWatcher) loop(ctx context.Context, src DirEventSource, done chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			w.warn("start watcher goroutine panic: %v", r)
		}

		close(done)
	}()

	events := src.Events()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}

			w.handleEvent(ev)
		}
	}
}

func (w *StartWatcher) handleEvent(ev Event) {
	if ev.Op&EventCreate == 0 {
		return
	}

	// Only act on the terminal xcbuilddata dir — the recursive FSEvents
	// watch delivers it directly, no need to chase intermediate dirs.
	base := filepath.Base(ev.Path)
	if !strings.HasSuffix(base, xcbuilddataSuffix) {
		return
	}

	w.emitStartOnce(ev.Path)
}

// markHexSeen returns true the first time xcbuildDataDir is seen. De-duplicates
// across the seed walk and the notify stream (both can surface the same path).
func (w *StartWatcher) markHexSeen(xcbuildDataDir string) bool {
	w.seenMu.Lock()
	defer w.seenMu.Unlock()

	if w.seenHexDirs == nil {
		w.seenHexDirs = make(map[string]struct{})
	}

	if _, ok := w.seenHexDirs[xcbuildDataDir]; ok {
		return false
	}

	w.seenHexDirs[xcbuildDataDir] = struct{}{}

	return true
}

// emitStartOnce loads the build-request and emits a Start, guaranteeing a
// single OnStart per xcbuilddata dir path. The seen-set is marked only after
// a successful classify, so a premature call (Load returns ErrNotFound before
// build-request.json lands) does not poison a later retry.
func (w *StartWatcher) emitStartOnce(xcbuildDataDir string) {
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
		w.debugf("StartWatcher: reject hex=%s action=%s qos=%s (not primary)", hex, info.Action, info.QoS)

		return
	}

	if !w.markHexSeen(xcbuildDataDir) {
		return
	}

	if w.OnStart == nil {
		return
	}

	w.debugf("StartWatcher: emit hex=%s action=%s qos=%s", hex, info.Action, info.QoS)

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

func (w *StartWatcher) debugf(format string, args ...any) {
	if w.Logger == nil {
		return
	}

	w.Logger.Debugf(format, args...)
}
