// Package recwatch is a pure-Go recursive filesystem watcher built on
// fsnotify. Covers the two cases that bit us with the flat watcher:
//   - deep-tree atomic creation (mkdir -p a/b/c/file): one real event
//     fires on a, descendants surface via a synchronous walk + synthetic
//     event replay at CREATE time.
//   - subscribing to a root that does not yet exist: Add makes the dir if
//     missing before the initial walk.
package recwatch

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

type Op uint32

const (
	Create Op = 1 << iota
	Write
	Remove
	Rename
)

type Event struct {
	Path string
	Op   Op
}

type Watcher struct {
	fs     *fsnotify.Watcher
	events chan Event
	ops    Op

	mu      sync.Mutex
	watched map[string]struct{}
	closed  bool
	wg      sync.WaitGroup
}

const defaultEventBuffer = 128

// New constructs a Watcher that emits events matching any of ops. The caller
// subscribes dirs via Add. events channel capacity is modest; callers should
// drain promptly.
func New(ops Op) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("fsnotify new: %w", err)
	}

	w := &Watcher{
		fs:      fsw,
		events:  make(chan Event, defaultEventBuffer),
		ops:     ops,
		watched: make(map[string]struct{}),
	}

	w.wg.Add(1)
	go w.loop()

	return w, nil
}

func (w *Watcher) Events() <-chan Event { return w.events }

// Add subscribes root and every dir beneath it. Missing root is created.
// Existing descendants are left for the caller to discover via a seed walk —
// Add emits NO synthetic events to avoid double-firing against the walker.
func (w *Watcher) Add(root string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", root, err)
	}

	return w.addSubtreeSilent(root)
}

func (w *Watcher) addSubtreeSilent(root string) error {
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable subtree, keep walking
		}
		if !d.IsDir() {
			return nil
		}

		return w.addOne(path)
	})
	if err != nil {
		return fmt.Errorf("walk %s: %w", root, err)
	}

	return nil
}

func (w *Watcher) addOne(dir string) error {
	w.mu.Lock()
	if _, ok := w.watched[dir]; ok {
		w.mu.Unlock()

		return nil
	}
	w.watched[dir] = struct{}{}
	w.mu.Unlock()

	if err := w.fs.Add(dir); err != nil {
		w.mu.Lock()
		delete(w.watched, dir)
		w.mu.Unlock()
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrPermission) {
			return nil
		}

		return fmt.Errorf("fsnotify add %s: %w", dir, err)
	}

	return nil
}

// addSubtreeAndReplay is the CREATE-time path: it adds every descendant dir
// (including root) to the watch AND emits synthetic CREATE events for every
// descendant EXCEPT root — root's event is the real fsnotify one the handler
// already emitted. Covers the atomic-deep-mkdir race where descendants land
// before our Add call reaches them.
func (w *Watcher) addSubtreeAndReplay(root string) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable subtree, keep walking
		}

		if d.IsDir() {
			_ = w.addOne(path)
		}

		if path != root {
			w.emit(Event{Path: path, Op: Create})
		}

		return nil
	})
}

func (w *Watcher) emit(ev Event) {
	if ev.Op&w.ops == 0 {
		return
	}

	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return
	}

	select {
	case w.events <- ev:
	default:
	}
}

func (w *Watcher) loop() {
	defer w.wg.Done()

	for {
		select {
		case ev, ok := <-w.fs.Events:
			if !ok {
				w.closeEvents()

				return
			}
			w.handle(ev)
		case err, ok := <-w.fs.Errors:
			if !ok {
				w.closeEvents()

				return
			}
			_ = err
		}
	}
}

func (w *Watcher) handle(ev fsnotify.Event) {
	op := translate(ev.Op)
	if op == 0 {
		return
	}

	if op&Create != 0 {
		if info, err := os.Lstat(ev.Name); err == nil && info.IsDir() {
			w.emit(Event{Path: ev.Name, Op: Create})
			w.addSubtreeAndReplay(ev.Name)

			return
		}
	}

	if op&Remove != 0 {
		w.mu.Lock()
		delete(w.watched, ev.Name)
		w.mu.Unlock()
	}

	w.emit(Event{Path: ev.Name, Op: op})
}

func translate(fsop fsnotify.Op) Op {
	var op Op
	if fsop&fsnotify.Create != 0 {
		op |= Create
	}
	if fsop&fsnotify.Write != 0 {
		op |= Write
	}
	if fsop&fsnotify.Remove != 0 {
		op |= Remove
	}
	if fsop&fsnotify.Rename != 0 {
		op |= Rename
	}

	return op
}

func (w *Watcher) closeEvents() {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.events)
	}
	w.mu.Unlock()
}

// Close stops the watcher and drains the loop. Safe to call multiple times.
func (w *Watcher) Close() {
	_ = w.fs.Close()
	w.wg.Wait()
}
