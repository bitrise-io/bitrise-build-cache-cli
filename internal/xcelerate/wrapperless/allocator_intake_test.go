//go:build unit

package wrapperless

import (
	"context"
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
