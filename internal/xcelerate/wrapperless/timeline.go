package wrapperless

import (
	"context"
	"sync"
)

const timelineCapacity = 10_000

// EventKind tags which signal struct a timelineEvent carries so the
// Allocator's single-reader goroutine can dispatch without a type switch
// across the public signal types.
type EventKind int

const (
	EventKindStart EventKind = iota
	EventKindEnd
	EventKindQuit
)

type TimelineEvent struct {
	Kind  EventKind
	Start Start
	End   End
	Quit  Quit
}

type Timeline struct {
	ch chan TimelineEvent

	closeOnce sync.Once
	closed    chan struct{}
}

func NewTimeline() *Timeline {
	return &Timeline{
		ch:     make(chan TimelineEvent, timelineCapacity),
		closed: make(chan struct{}),
	}
}

func (t *Timeline) Events() <-chan TimelineEvent {
	return t.ch
}

func (t *Timeline) PushStart(s Start) { t.push(TimelineEvent{Kind: EventKindStart, Start: s}) }
func (t *Timeline) PushEnd(e End)     { t.push(TimelineEvent{Kind: EventKindEnd, End: e}) }
func (t *Timeline) PushQuit(q Quit)   { t.push(TimelineEvent{Kind: EventKindQuit, Quit: q}) }

// push drops the event if the timeline is closed or the buffer is full.
// Overflow is intentional: the pipeline is a best-effort attributor, not an
// at-least-once queue.
func (t *Timeline) push(ev TimelineEvent) {
	select {
	case <-t.closed:
		return
	default:
	}

	select {
	case t.ch <- ev:
	default:
	}
}

func (t *Timeline) Close() {
	t.closeOnce.Do(func() {
		close(t.closed)
		close(t.ch)
	})
}

// Drain forwards events from t into onEvent until ctx is cancelled or the
// channel is closed. Single-reader by design — Allocator owns the drain.
func (t *Timeline) Drain(ctx context.Context, onEvent func(TimelineEvent)) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-t.ch:
			if !ok {
				return
			}
			onEvent(ev)
		}
	}
}
