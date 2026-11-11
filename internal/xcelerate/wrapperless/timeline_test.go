//go:build unit

package wrapperless

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTimeline_PushAndDrain(t *testing.T) {
	tl := NewTimeline()
	defer tl.Close()

	tl.PushStart(Start{BuildRequestHex: "aa"})
	tl.PushEnd(End{UniqueIdentifier: "x"})
	tl.PushQuit(Quit{PID: 1})

	var mu sync.Mutex
	got := make([]EventKind, 0, 3)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		tl.Drain(ctx, func(ev TimelineEvent) {
			mu.Lock()
			got = append(got, ev.Kind)
			if len(got) == 3 {
				cancel()
			}
			mu.Unlock()
		})
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("drain stuck")
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []EventKind{EventKindStart, EventKindEnd, EventKindQuit}, got)
}

func TestTimeline_PushAfterCloseDoesNotPanic(t *testing.T) {
	tl := NewTimeline()
	tl.Close()
	tl.PushStart(Start{})
	tl.PushEnd(End{})
	tl.PushQuit(Quit{})
}

func TestTimeline_DrainExhaustsBufferAfterContextCancel(t *testing.T) {
	tl := NewTimeline()

	tl.PushStart(Start{BuildRequestHex: "1"})
	tl.PushEnd(End{UniqueIdentifier: "2"})
	tl.PushQuit(Quit{PID: 3})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tl.Close()

	var mu sync.Mutex
	kinds := make([]EventKind, 0, 3)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tl.Drain(ctx, func(ev TimelineEvent) {
			mu.Lock()
			kinds = append(kinds, ev.Kind)
			mu.Unlock()
		})
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("drain did not return")
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []EventKind{EventKindStart, EventKindEnd, EventKindQuit}, kinds,
		"buffered events must drain even when ctx is already cancelled")
}
