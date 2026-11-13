//go:build darwin

package wrapperless

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

type kqueueExitNotifier struct {
	kq     int
	events chan int

	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

// newExitNotifier returns the darwin kqueue-backed exit notifier. Events
// arrive on Events() the moment the kernel reaps the exited PID — no polling
// interval, no race window against a trailing Quit vs. a freshly-opened
// bucket.
func newExitNotifier() (ExitNotifier, error) {
	kq, err := unix.Kqueue()
	if err != nil {
		return nil, fmt.Errorf("kqueue: %w", err)
	}

	n := &kqueueExitNotifier{
		kq:     kq,
		events: make(chan int, exitEventBuffer),
	}

	n.wg.Add(1)
	go n.pump()

	return n, nil
}

const exitEventBuffer = 64

func (n *kqueueExitNotifier) Watch(pid int) error {
	if pid <= 0 {
		return nil
	}

	kev := unix.Kevent_t{
		Ident:  uint64(pid),
		Filter: unix.EVFILT_PROC,
		Flags:  unix.EV_ADD | unix.EV_ENABLE | unix.EV_ONESHOT,
		Fflags: unix.NOTE_EXIT,
	}

	_, err := unix.Kevent(n.kq, []unix.Kevent_t{kev}, nil, nil)
	if err != nil {
		if errors.Is(err, syscall.ESRCH) {
			// PID already gone — treat as fired-and-forgotten.
			select {
			case n.events <- pid:
			default:
			}

			return nil
		}

		return fmt.Errorf("kevent register pid=%d: %w", pid, err)
	}

	return nil
}

func (n *kqueueExitNotifier) Unwatch(pid int) {
	if pid <= 0 {
		return
	}

	kev := unix.Kevent_t{
		Ident:  uint64(pid),
		Filter: unix.EVFILT_PROC,
		Flags:  unix.EV_DELETE,
	}

	_, _ = unix.Kevent(n.kq, []unix.Kevent_t{kev}, nil, nil)
}

func (n *kqueueExitNotifier) Events() <-chan int { return n.events }

func (n *kqueueExitNotifier) Close() {
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()

		return
	}
	n.closed = true
	n.mu.Unlock()

	_ = unix.Close(n.kq)
	n.wg.Wait()
	close(n.events)
}

func (n *kqueueExitNotifier) pump() {
	defer n.wg.Done()

	events := make([]unix.Kevent_t, 16)

	for {
		cnt, err := unix.Kevent(n.kq, nil, events, nil)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}

			return
		}

		for i := range cnt {
			ev := events[i]
			if ev.Filter != unix.EVFILT_PROC || ev.Fflags&unix.NOTE_EXIT == 0 {
				continue
			}
			if ev.Ident > math.MaxInt32 {
				continue
			}

			n.mu.Lock()
			closed := n.closed
			n.mu.Unlock()
			if closed {
				return
			}

			select {
			case n.events <- int(ev.Ident): //nolint:gosec // bounded via MaxInt32 check above
			default:
			}
		}
	}
}
