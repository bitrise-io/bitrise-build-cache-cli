//go:build unit

package wrapperless

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestQuitWatcher_FiresOnDeadPID(t *testing.T) {
	alive := map[int]bool{100: true, 200: true}
	var mu sync.Mutex

	quits := make(chan Quit, 4)

	w := &QuitWatcher{
		PollInterval: 5 * time.Millisecond,
		OnQuit:       func(q Quit) { quits <- q },
		IsAlive: func(pid int) bool {
			mu.Lock()
			defer mu.Unlock()

			return alive[pid]
		},
	}
	w.Register(100, AncestryIDE)
	w.Register(200, AncestryCLI)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	mu.Lock()
	alive[100] = false
	mu.Unlock()

	select {
	case q := <-quits:
		assert.Equal(t, 100, q.PID)
		assert.True(t, q.AncestryIDE)
	case <-time.After(1 * time.Second):
		t.Fatal("no Quit for 100")
	}

	mu.Lock()
	alive[200] = false
	mu.Unlock()

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

	w.Register(0, AncestryIDE)
	w.Register(-1, AncestryIDE)
	w.Register(5, AncestryUnknown)

	w.mu.Lock()
	defer w.mu.Unlock()
	assert.Empty(t, w.watched)
}

func TestQuitWatcher_FiresOncePerPID(t *testing.T) {
	calls := 0
	quits := make(chan Quit, 4)

	w := &QuitWatcher{
		PollInterval: 5 * time.Millisecond,
		OnQuit: func(q Quit) {
			calls++
			quits <- q
		},
		IsAlive: func(_ int) bool { return false },
	}
	w.Register(42, AncestryCLI)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	select {
	case <-quits:
	case <-time.After(1 * time.Second):
		t.Fatal("no Quit")
	}

	time.Sleep(50 * time.Millisecond)

	w.Close()

	assert.Equal(t, 1, calls)
}

func TestQuitWatcher_CloseIdempotent(t *testing.T) {
	w := &QuitWatcher{}
	w.Close()
	w.Close()
}
