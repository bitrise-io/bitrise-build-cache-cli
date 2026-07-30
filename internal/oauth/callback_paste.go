package oauth

import (
	"bufio"
	"context"
	"fmt"
	"time"
)

type deadlineReader interface {
	SetReadDeadline(t time.Time) error
}

// pasteGrace is how long the loopback gets before stdin is touched. A browser
// that can reach the listener does so in well under this, so the reachable case
// never starts a reader at all.
const pasteGrace = 15 * time.Second

// awaitCallback waits for the authorization code from the loopback callback,
// falling back to a URL the user pastes in.
//
// The reader is armed only after pasteGrace, because a blocked read on a
// terminal cannot be cancelled — SetReadDeadline is unsupported on a character
// device — so an armed reader that outlives this call would race whatever prompt
// runs next for every keystroke. Deferring it means the common case (a browser
// that reaches the listener) never touches stdin, and the paste case has the
// reader finish on its own once it consumes the line. Terminal input is buffered,
// so a user who pastes during the grace period is still read.
func (c Config) awaitCallback(ctx context.Context, cs *callbackServer) (string, error) {
	if c.PasteReader == nil {
		return cs.wait(ctx)
	}

	graceFor := c.PasteGrace
	if graceFor <= 0 {
		graceFor = pasteGrace
	}

	grace := time.NewTimer(graceFor)
	defer grace.Stop()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("timed out waiting for the browser sign-in to complete: %w", ctx.Err())
	case res := <-cs.results:
		return res.code, res.err
	case <-grace.C:
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
//
// It reports failure loudly: a reader left blocked on a shared stdin competes
// for every byte with the next prompt, which looks like dropped keystrokes
// rather than an error.
func (c Config) stopPasteReader(done <-chan struct{}) {
	select {
	case <-done:
		return // already finished, nothing to unblock
	default:
	}

	dr, ok := c.PasteReader.(deadlineReader)
	if !ok {
		c.warnStdinUnreliable("this reader cannot be interrupted")

		return
	}

	if err := dr.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
		// Character devices reject deadlines, so this is the terminal case.
		c.warnStdinUnreliable(err.Error())

		return
	}
	<-done
	_ = dr.SetReadDeadline(time.Time{})
}

func (c Config) warnStdinUnreliable(reason string) {
	c.warnf("Could not stop reading standard input (%s).", reason)
	c.warnf("Later prompts in this command may drop keystrokes — pass --workspace to skip the picker.")
}
