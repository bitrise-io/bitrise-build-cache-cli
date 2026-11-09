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
	logger  log.Logger
	stopMu  sync.Mutex
	stopped bool
}

func newNotifyAdapter(logger log.Logger) *notifyAdapter {
	raw := make(chan notify.EventInfo, eventChanBuffer)
	out := make(chan Event, eventChanBuffer)
	a := &notifyAdapter{events: out, raw: raw, logger: logger}

	go a.pump()

	return a
}

func (a *notifyAdapter) pump() {
	for ev := range a.raw {
		if a.logger != nil {
			a.logger.Infof("notifyAdapter: raw event %v %s", ev.Event(), ev.Path())
		}

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
			if a.logger != nil {
				a.logger.Warnf("notifyAdapter: event chan full, dropping %s", ev.Path())
			}
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

	mu        sync.Mutex
	src       DirEventSource
	cancel    context.CancelFunc
	done      chan struct{}
	startedAt time.Time

	seenMu      sync.Mutex
	seenHexDirs map[string]struct{}
}

func (w *StartWatcher) Start(ctx context.Context) error {
	newSource := w.NewSource
	if newSource == nil {
		logger := w.Logger
		newSource = func() (DirEventSource, error) { return newNotifyAdapter(logger), nil }
	}

	src, err := newSource()
	if err != nil {
		w.warn("start watcher source init: %s", err)

		return nil
	}

	w.infof("StartWatcher: source init ok")

	ddRoot := w.derivedDataDir()
	w.infof("StartWatcher: dd root = %q", ddRoot)

	if ddRoot != "" {
		w.installWatch(src, ddRoot)
	}

	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	w.mu.Lock()
	w.src = src
	w.cancel = cancel
	w.done = done
	w.startedAt = w.now()
	w.mu.Unlock()

	// Seed-walk the DD tree for any xcbuilddata that already exists when
	// the watcher starts. Closes the window where Xcode created dirs
	// before the recursive watch was installed.
	seeded := 0
	if ddRoot != "" {
		seeded = w.seedWalk(ddRoot)
	}
	w.infof("StartWatcher: seed walk done (%d hex found)", seeded)

	go w.loop(ctx, src, done)
	w.infof("StartWatcher: loop goroutine started")

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

func (w *StartWatcher) installWatch(src DirEventSource, ddRoot string) {
	if mkErr := os.MkdirAll(ddRoot, 0o755); mkErr != nil {
		w.warn("start watcher mkdir %s: %s", ddRoot, mkErr)

		return
	}

	w.infof("StartWatcher: mkdir ok")

	if addErr := src.Watch(recursivePath(ddRoot), EventCreate); addErr != nil {
		w.warn("start watcher watch %s: %s", ddRoot, addErr)

		return
	}

	w.infof("StartWatcher: watching %s recursively", recursivePath(ddRoot))
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
// the notify stream via the seen-set. Skips xcbuilddata whose sibling
// LogStoreManifest.plist is already sealed and predates startedAt, so a
// previous Xcode session's leftovers don't fire a phantom START.
func (w *StartWatcher) seedWalk(ddRoot string) int {
	count := 0
	_ = filepath.WalkDir(ddRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			//nolint:nilerr // skip the un-readable subtree, keep walking siblings
			return filepath.SkipDir
		}
		if !d.IsDir() {
			return nil
		}

		if strings.HasSuffix(d.Name(), xcbuilddataSuffix) {
			if w.isStaleCompletedBuild(path) {
				w.infof("StartWatcher: skip seed %s reason=stale-sibling-manifest", path)

				return filepath.SkipDir
			}
			count++
			w.emitStartOnce(path)

			return filepath.SkipDir
		}

		return nil
	})

	return count
}

// isStaleCompletedBuild reports whether xcbuildDataDir belongs to a build whose
// Logs/Build/LogStoreManifest.plist predates the proxy by more than
// seedStaleGrace — i.e. a prior Xcode session's leftover that will never see
// new CAS traffic.
func (w *StartWatcher) isStaleCompletedBuild(xcbuildDataDir string) bool {
	if w.startedAt.IsZero() {
		return false
	}

	root := rootForXCBuildDataPath(xcbuildDataDir)
	manifest := filepath.Join(root, "Logs", buildDirName, "LogStoreManifest.plist")

	info, err := os.Stat(manifest)
	if err != nil {
		return false
	}

	return info.ModTime().Before(w.startedAt.Add(-seedStaleGrace))
}

func (w *StartWatcher) loop(ctx context.Context, src DirEventSource, done chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			w.warn("start watcher goroutine panic: %v", r)
		}

		close(done)
	}()

	w.infof("StartWatcher: loop entered")

	events := src.Events()
	eventCount := 0

	for {
		select {
		case <-ctx.Done():
			w.infof("StartWatcher: ctx done, exiting")

			return
		case ev, ok := <-events:
			if !ok {
				w.infof("StartWatcher: event chan closed, exiting")

				return
			}

			eventCount++
			w.infof("StartWatcher: event #%d op=%d %s", eventCount, ev.Op, ev.Path)
			w.handleEvent(ev)
		}
	}
}

func (w *StartWatcher) handleEvent(ev Event) {
	w.infof("StartWatcher: handleEvent %s", ev.Path)

	if ev.Op&EventCreate == 0 {
		return
	}

	// Act on the terminal xcbuilddata dir — the recursive FSEvents watch
	// delivers it directly. Also retry when build-request.json itself
	// shows up: when Xcode writes the file after the dir CREATE, the
	// initial emitStartOnce call hits ErrNotFound and the seen-set stays
	// unmarked, so this second pass against the parent dir succeeds.
	base := filepath.Base(ev.Path)
	switch {
	case strings.HasSuffix(base, xcbuilddataSuffix):
		w.emitStartOnce(ev.Path)
	case base == "build-request.json":
		w.emitStartOnce(filepath.Dir(ev.Path))
	}
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

func (w *StartWatcher) infof(format string, args ...any) {
	if w.Logger == nil {
		return
	}

	w.Logger.Infof(format, args...)
}

func (w *StartWatcher) debugf(format string, args ...any) {
	if w.Logger == nil {
		return
	}

	w.Logger.Debugf(format, args...)
}
