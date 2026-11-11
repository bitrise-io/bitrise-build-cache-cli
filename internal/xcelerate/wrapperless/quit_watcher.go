package wrapperless

import (
	"context"
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

type Ancestry int

const (
	AncestryUnknown Ancestry = iota
	AncestryIDE
	AncestryCLI
)

// NewNotifier is a seam for tests to inject a fake ExitNotifier. nil → use
// the platform-default (kqueue on darwin, poll elsewhere).
type QuitWatcher struct {
	Logger      log.Logger
	OnQuit      func(Quit)
	Now         func() time.Time
	NewNotifier func() (ExitNotifier, error)

	mu       sync.Mutex
	watched  map[int]watchedPeer
	notifier ExitNotifier
	cancel   context.CancelFunc
	done     chan struct{}
	started  bool
}

type watchedPeer struct {
	Ancestry    Ancestry
	StartTimeMS int64
}

func (w *QuitWatcher) Register(pid int, ancestry Ancestry, startTimeMS int64) {
	if pid <= 0 || ancestry == AncestryUnknown {
		return
	}

	w.mu.Lock()
	if w.watched == nil {
		w.watched = make(map[int]watchedPeer)
	}
	w.watched[pid] = watchedPeer{Ancestry: ancestry, StartTimeMS: startTimeMS}
	notifier := w.notifier
	w.mu.Unlock()

	if notifier != nil {
		if err := notifier.Watch(pid); err != nil {
			w.warn("quit watcher notifier watch pid=%d: %s", pid, err)
		}
	}
}

func (w *QuitWatcher) Start(ctx context.Context) {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()

		return
	}
	w.started = true

	if w.watched == nil {
		w.watched = make(map[int]watchedPeer)
	}

	factory := w.NewNotifier
	if factory == nil {
		factory = newExitNotifier
	}

	notifier, err := factory()
	if err != nil {
		w.warn("quit watcher notifier init: %s", err)
		w.started = false
		w.mu.Unlock()

		return
	}
	w.notifier = notifier

	// Arm notifier for peers that registered before Start.
	for pid := range w.watched {
		if err := notifier.Watch(pid); err != nil {
			w.warn("quit watcher notifier watch pid=%d: %s", pid, err)
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	done := make(chan struct{})
	w.done = done
	w.mu.Unlock()

	go w.loop(ctx, notifier, done)
}

func (w *QuitWatcher) Close() {
	w.mu.Lock()
	cancel := w.cancel
	done := w.done
	notifier := w.notifier
	w.cancel = nil
	w.done = nil
	w.notifier = nil
	w.started = false
	w.mu.Unlock()

	if cancel == nil {
		return
	}

	cancel()

	if notifier != nil {
		notifier.Close()
	}

	if done != nil {
		<-done
	}
}

func (w *QuitWatcher) loop(ctx context.Context, notifier ExitNotifier, done chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			w.warn("quit watcher goroutine panic: %v", r)
		}

		close(done)
	}()

	events := notifier.Events()

	for {
		select {
		case <-ctx.Done():
			return
		case pid, ok := <-events:
			if !ok {
				return
			}
			w.emit(pid)
		}
	}
}

func (w *QuitWatcher) emit(pid int) {
	w.mu.Lock()
	peer, ok := w.watched[pid]
	if ok {
		delete(w.watched, pid)
	}
	w.mu.Unlock()

	if !ok || w.OnQuit == nil {
		return
	}

	w.OnQuit(Quit{
		At:          w.now(),
		PID:         pid,
		StartTimeMS: peer.StartTimeMS,
		AncestryIDE: peer.Ancestry == AncestryIDE,
	})
}

func (w *QuitWatcher) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}

	return time.Now()
}

func (w *QuitWatcher) warn(format string, args ...any) {
	if w.Logger == nil {
		return
	}

	w.Logger.Warnf(format, args...)
}
