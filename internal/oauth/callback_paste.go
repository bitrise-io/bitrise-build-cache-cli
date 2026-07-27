package oauth

import (
	"bufio"
	"context"
	"fmt"
	"time"
)

// deadlineReader is satisfied by *os.File on pollable fds (terminals, pipes),
// which is how a blocked read on stdin gets unblocked once the loopback
// callback wins the race.
type deadlineReader interface {
	SetReadDeadline(t time.Time) error
}

// awaitCallback waits for the authorization code, from the loopback callback or
// — when PasteReader is set — from a callback URL the user pastes in, whichever
// arrives first.
func (c Config) awaitCallback(ctx context.Context, cs *callbackServer) (string, error) {
	if c.PasteReader == nil {
		return cs.wait(ctx)
	}

	pasted := make(chan callbackResult, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.readPastedCallback(cs.state, pasted)
	}()
	defer c.stopPasteReader(done)

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("timed out waiting for the browser sign-in to complete: %w", ctx.Err())
	case res := <-cs.results:
		return res.code, res.err
	case res := <-pasted:
		return res.code, res.err
	}
}

// readPastedCallback parses each line as a callback URL, delivering the first
// conclusive one (a code, or an authorization error from the provider) and
// nudging the user on anything unparseable.
func (c Config) readPastedCallback(state string, out chan<- callbackResult) {
	scanner := bufio.NewScanner(c.PasteReader)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		res, retry := parsePastedCallback(line, state)
		if retry {
			c.warnf("That doesn't look like the callback URL (%s). Paste the full URL from the browser's address bar.", res.err)

			continue
		}

		out <- res

		return
	}
}

// stopPasteReader unblocks the reader's pending read so it can't linger and
// steal keystrokes from later prompts, then restores blocking reads.
func (c Config) stopPasteReader(done <-chan struct{}) {
	dr, ok := c.PasteReader.(deadlineReader)
	if !ok {
		return // not pollable: nothing to interrupt, and no interactive prompts follow
	}

	if err := dr.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
		return
	}
	<-done
	_ = dr.SetReadDeadline(time.Time{})
}
