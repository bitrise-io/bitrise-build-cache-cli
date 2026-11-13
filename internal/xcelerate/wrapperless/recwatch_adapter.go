package wrapperless

import (
	"strings"
	"sync"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/wrapperless/recwatch"
)

// recwatchAdapter wraps recwatch.Watcher to satisfy DirEventSource. Replaces
// rjeczalik/notify which requires CGO on darwin for FSEvents.
type recwatchAdapter struct {
	events chan Event
	logger log.Logger

	mu       sync.Mutex
	inner    *recwatch.Watcher
	stopped  bool
	stopOnce sync.Once
}

func newRecwatchAdapter(logger log.Logger) *recwatchAdapter {
	return &recwatchAdapter{
		events: make(chan Event, eventChanBuffer),
		logger: logger,
	}
}

func (a *recwatchAdapter) Events() <-chan Event { return a.events }

func (a *recwatchAdapter) Watch(root string, ops ...EventOp) error {
	a.mu.Lock()
	if a.inner == nil {
		inner, err := recwatch.New(toRecwatchOps(ops))
		if err != nil {
			a.mu.Unlock()

			return err //nolint:wrapcheck // recwatch error already contextualised
		}
		a.inner = inner

		go a.pump()
	}
	a.mu.Unlock()

	// recwatch accepts recursive roots natively via its walk-on-create path;
	// strip the notify-style "..." suffix the callers still append.
	root = strings.TrimSuffix(root, "/...")

	return a.inner.Add(root) //nolint:wrapcheck // recwatch error already contextualised
}

func (a *recwatchAdapter) pump() {
	for ev := range a.inner.Events() {
		if a.logger != nil {
			a.logger.Debugf("recwatchAdapter: event op=%d %s", ev.Op, ev.Path)
		}
		select {
		case a.events <- Event{Path: ev.Path, Op: fromRecwatchOp(ev.Op)}:
		default:
			if a.logger != nil {
				a.logger.Warnf("recwatchAdapter: event chan full, dropping %s", ev.Path)
			}
		}
	}

	a.mu.Lock()
	if !a.stopped {
		a.stopped = true
		close(a.events)
	}
	a.mu.Unlock()
}

func (a *recwatchAdapter) Stop() {
	a.stopOnce.Do(func() {
		a.mu.Lock()
		inner := a.inner
		a.mu.Unlock()
		if inner != nil {
			inner.Close()
		}
	})
}

func toRecwatchOps(ops []EventOp) recwatch.Op {
	var out recwatch.Op
	for _, op := range ops {
		if op&EventCreate != 0 {
			out |= recwatch.Create
		}
		if op&EventWrite != 0 {
			out |= recwatch.Write
		}
	}

	return out
}

func fromRecwatchOp(op recwatch.Op) EventOp {
	var out EventOp
	if op&recwatch.Create != 0 {
		out |= EventCreate
	}
	if op&recwatch.Write != 0 {
		out |= EventWrite
	}

	return out
}
