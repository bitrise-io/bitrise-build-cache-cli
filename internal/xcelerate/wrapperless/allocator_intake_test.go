//go:build unit

package wrapperless

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestAllocator_PushAfterCloseIntakeDoesNotPanic(t *testing.T) {
	a := NewAllocator("h", 1, 0)
	a.CloseIntake()
	a.PushStart(Start{})
	a.PushEnd(End{})
	a.PushQuit(Quit{})
}

// TestAllocator_ConcurrentPushAndCloseIntakeNeverPanics exercises the race
// between a producer goroutine already past its intakeDone check and
// CloseIntake firing in another goroutine. The intake channel is deliberately
// never closed to make send-on-closed impossible — this test is the regression
// guard against a future author re-introducing close(a.intake) inside
// CloseIntake.
func TestAllocator_ConcurrentPushAndCloseIntakeNeverPanics(t *testing.T) {
	a := NewAllocator("h", 1, 0)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 128; j++ {
				a.PushStart(Start{BuildRequestHex: "aa"})
			}
		}()
	}

	time.Sleep(time.Millisecond)
	a.CloseIntake()
	wg.Wait()
}

// TestAllocator_RunReturnsAfterCloseIntake pins the drain's close-exit path:
// Run must return as soon as CloseIntake closes the channel, after any
// already-buffered events finish dispatching through Handle.
func TestAllocator_RunReturnsAfterCloseIntake(t *testing.T) {
	a := NewAllocator("h", 1, 0)

	a.PushStart(Start{BuildRequestHex: "aa", Action: PrimaryActionBuild})
	a.PushEnd(End{UniqueIdentifier: "x", Success: true})

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.Run(context.Background())
	}()

	a.CloseIntake()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Run did not return after CloseIntake")
	}
}

// TestAllocator_RunExhaustsBufferOnCtxCancel pins the drain's cancel-exhaust
// path: ctx.Done() must still consume already-buffered events before Run
// returns, so a mid-flight End queued behind a slow enriched PUT is not
// silently dropped on shutdown.
func TestAllocator_RunExhaustsBufferOnCtxCancel(t *testing.T) {
	a := NewAllocator("h", 1, 0)

	a.PushStart(Start{BuildRequestHex: "aa", Action: PrimaryActionBuild})
	a.PushEnd(End{UniqueIdentifier: "x", Success: true})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.Run(ctx)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}
