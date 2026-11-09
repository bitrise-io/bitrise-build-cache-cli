package wrapperless

import (
	"context"
	"sync"
	"syscall"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

const quitPollInterval = 1 * time.Second

type Ancestry int

const (
	AncestryUnknown Ancestry = iota
	AncestryIDE
	AncestryCLI
)

type QuitWatcher struct {
	Logger       log.Logger
	OnQuit       func(Quit)
	PollInterval time.Duration
	Now          func() time.Time
	IsAlive      func(pid int) bool

	mu      sync.Mutex
	watched map[int]Ancestry
	cancel  context.CancelFunc
	done    chan struct{}
	ticker  *time.Ticker
	started bool
}

func (w *QuitWatcher) Register(pid int, ancestry Ancestry) {
	if pid <= 0 || ancestry == AncestryUnknown {
		return
	}

	w.mu.Lock()
	if w.watched == nil {
		w.watched = make(map[int]Ancestry)
	}
	w.watched[pid] = ancestry
	w.mu.Unlock()
}

func (w *QuitWatcher) Start(ctx context.Context) {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()

		return
	}
	w.started = true

	if w.watched == nil {
		w.watched = make(map[int]Ancestry)
	}

	interval := w.PollInterval
	if interval <= 0 {
		interval = quitPollInterval
	}
	ticker := time.NewTicker(interval)
	w.ticker = ticker

	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	done := make(chan struct{})
	w.done = done
	w.mu.Unlock()

	go w.loop(ctx, ticker, done)
}

func (w *QuitWatcher) Close() {
	w.mu.Lock()
	cancel := w.cancel
	done := w.done
	ticker := w.ticker
	w.cancel = nil
	w.done = nil
	w.ticker = nil
	w.started = false
	w.mu.Unlock()

	if cancel == nil {
		return
	}

	cancel()

	if ticker != nil {
		ticker.Stop()
	}

	if done != nil {
		<-done
	}
}

func (w *QuitWatcher) loop(ctx context.Context, ticker *time.Ticker, done chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			w.warn("quit watcher goroutine panic: %v", r)
		}

		close(done)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.scan()
		}
	}
}

// Wrapper is pulled out so tests can stub by assigning IsAlive.
func (w *QuitWatcher) alive(pid int) bool {
	if w.IsAlive != nil {
		return w.IsAlive(pid)
	}

	return defaultIsAlive(pid)
}

// defaultIsAlive sends signal 0 to pid; absent errors or EPERM mean the
// process still exists. ESRCH means the PID is gone. Any other error we treat
// as "still alive" so we don't fire spurious Quits on transient kernel
// errors.
func defaultIsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}

	return err != syscall.ESRCH
}

func (w *QuitWatcher) scan() {
	w.mu.Lock()
	snapshot := make(map[int]Ancestry, len(w.watched))
	for pid, anc := range w.watched {
		snapshot[pid] = anc
	}
	w.mu.Unlock()

	var dead []int
	for pid := range snapshot {
		if !w.alive(pid) {
			dead = append(dead, pid)
		}
	}

	if len(dead) == 0 {
		return
	}

	w.mu.Lock()
	for _, pid := range dead {
		delete(w.watched, pid)
	}
	w.mu.Unlock()

	if w.OnQuit == nil {
		return
	}

	now := w.now()
	for _, pid := range dead {
		w.OnQuit(Quit{
			At:          now,
			PID:         pid,
			AncestryIDE: snapshot[pid] == AncestryIDE,
		})
	}
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
