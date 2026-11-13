//go:build !darwin

package wrapperless

import (
	"sync"
	"syscall"
	"time"
)

type pollExitNotifier struct {
	events chan int
	stop   chan struct{}
	wg     sync.WaitGroup

	mu      sync.Mutex
	watched map[int]struct{}
	closed  bool
}

const pollExitInterval = 500 * time.Millisecond

// newExitNotifier returns the non-darwin polling fallback. Kept for parity
// with the darwin kqueue path; the wrapperless pipeline is darwin-targeted in
// production, so the race-window concerns the kqueue path addresses do not
// apply on this build.
func newExitNotifier() (ExitNotifier, error) {
	n := &pollExitNotifier{
		events:  make(chan int, exitEventBuffer),
		stop:    make(chan struct{}),
		watched: make(map[int]struct{}),
	}

	n.wg.Add(1)
	go n.pump()

	return n, nil
}

const exitEventBuffer = 64

func (n *pollExitNotifier) Watch(pid int) error {
	if pid <= 0 {
		return nil
	}

	n.mu.Lock()
	n.watched[pid] = struct{}{}
	n.mu.Unlock()

	return nil
}

func (n *pollExitNotifier) Unwatch(pid int) {
	n.mu.Lock()
	delete(n.watched, pid)
	n.mu.Unlock()
}

func (n *pollExitNotifier) Events() <-chan int { return n.events }

func (n *pollExitNotifier) Close() {
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()

		return
	}
	n.closed = true
	n.mu.Unlock()

	close(n.stop)
	n.wg.Wait()
	close(n.events)
}

func (n *pollExitNotifier) pump() {
	defer n.wg.Done()

	ticker := time.NewTicker(pollExitInterval)
	defer ticker.Stop()

	for {
		select {
		case <-n.stop:
			return
		case <-ticker.C:
			n.scan()
		}
	}
}

func (n *pollExitNotifier) scan() {
	n.mu.Lock()
	snap := make([]int, 0, len(n.watched))
	for pid := range n.watched {
		snap = append(snap, pid)
	}
	n.mu.Unlock()

	for _, pid := range snap {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			n.mu.Lock()
			delete(n.watched, pid)
			n.mu.Unlock()
			select {
			case n.events <- pid:
			default:
			}
		}
	}
}
