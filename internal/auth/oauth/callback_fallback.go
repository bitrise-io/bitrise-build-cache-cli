package oauth

import (
	"context"
	"fmt"
	"time"
)

// CallbackFallback supplies the authorization code when the browser cannot reach
// the loopback listener — the caller reads it from wherever it can (a URL the
// user pastes into the terminal, typically). It runs concurrently with the
// listener and MUST return once ctx is cancelled, which happens as soon as the
// listener wins the race: until it does, it may still hold whatever it reads
// from, and on a terminal that means competing with the next prompt for input.
//
// Whatever the caller reads goes through ParsePastedCallback, so the state check
// stays here with the rest of the flow.
type CallbackFallback func(ctx context.Context, state string) (string, error)

// fallbackExitGrace bounds the wait for a cancelled fallback so a misbehaving one
// can't wedge the sign-in.
const fallbackExitGrace = 3 * time.Second

// awaitCallback takes the authorization code from whichever of the loopback
// listener and the fallback produces one first.
func (c Config) awaitCallback(ctx context.Context, cs *callbackServer) (string, error) {
	if c.CallbackFallback == nil {
		return cs.wait(ctx)
	}

	fallbackCtx, cancel := context.WithCancel(ctx)
	fallback := make(chan callbackResult, 1)
	done := make(chan struct{})

	go func() {
		defer close(done)
		code, err := c.CallbackFallback(fallbackCtx, cs.state)
		fallback <- callbackResult{code: code, err: err}
	}()

	// Returning before the fallback has released its input source would leave it
	// racing the caller's next prompt for keystrokes.
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(fallbackExitGrace):
			c.warnf("The sign-in fallback did not stop when asked; later prompts in this command may drop keystrokes.")
		}
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("timed out waiting for the browser sign-in to complete: %w", ctx.Err())
	case res := <-cs.results:
		return res.code, res.err
	case res := <-fallback:
		return res.code, res.err
	}
}
