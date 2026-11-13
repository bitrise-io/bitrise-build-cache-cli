//go:build unit

package wrapperless

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type fakeExitNotifier struct {
	mu      sync.Mutex
	events  chan int
	watched map[int]struct{}
	closed  bool
}

func newFakeExitNotifier() *fakeExitNotifier {
	return &fakeExitNotifier{
		events:  make(chan int, 16),
		watched: make(map[int]struct{}),
	}
}

func (f *fakeExitNotifier) Watch(pid int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watched[pid] = struct{}{}

	return nil
}

func (f *fakeExitNotifier) Unwatch(pid int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.watched, pid)
}

func (f *fakeExitNotifier) Events() <-chan int { return f.events }

func (f *fakeExitNotifier) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.closed = true
	close(f.events)
}

func (f *fakeExitNotifier) fire(pid int) {
	f.mu.Lock()
	closed := f.closed
	f.mu.Unlock()
	if closed {
		return
	}
	f.events <- pid
}

func TestQuitWatcher_FiresOnExitEvent(t *testing.T) {
	src := newFakeExitNotifier()
	quits := make(chan Quit, 4)

	w := &QuitWatcher{
		OnQuit:      func(q Quit) { quits <- q },
		NewNotifier: func() (ExitNotifier, error) { return src, nil },
	}
	w.Register(100, AncestryIDE, 0)
	w.Register(200, AncestryCLI, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	src.fire(100)

	select {
	case q := <-quits:
		assert.Equal(t, 100, q.PID)
		assert.True(t, q.AncestryIDE)
	case <-time.After(1 * time.Second):
		t.Fatal("no Quit for 100")
	}

	src.fire(200)

	select {
	case q := <-quits:
		assert.Equal(t, 200, q.PID)
		assert.False(t, q.AncestryIDE)
	case <-time.After(1 * time.Second):
		t.Fatal("no Quit for 200")
	}

	w.Close()
}

func TestQuitWatcher_RegisterRejectsZero(t *testing.T) {
	w := &QuitWatcher{}

	w.Register(0, AncestryIDE, 0)
	w.Register(-1, AncestryIDE, 0)
	w.Register(5, AncestryUnknown, 0)

	w.mu.Lock()
	defer w.mu.Unlock()
	assert.Empty(t, w.watched)
}

func TestQuitWatcher_FiresOncePerPID(t *testing.T) {
	src := newFakeExitNotifier()
	calls := 0
	quits := make(chan Quit, 4)

	w := &QuitWatcher{
		OnQuit: func(q Quit) {
			calls++
			quits <- q
		},
		NewNotifier: func() (ExitNotifier, error) { return src, nil },
	}
	w.Register(42, AncestryCLI, 999)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	src.fire(42)

	select {
	case <-quits:
	case <-time.After(1 * time.Second):
		t.Fatal("no Quit")
	}

	src.fire(42)
	time.Sleep(50 * time.Millisecond)

	w.Close()

	assert.Equal(t, 1, calls)
}

func TestQuitWatcher_CloseIdempotent(t *testing.T) {
	w := &QuitWatcher{}
	w.Close()
	w.Close()
}
