package common

import (
	"bufio"
	"context"
	"io"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/oauth"
)

// pasteGrace is how long the loopback gets before stdin is touched. A browser
// that can reach the listener does so in well under this, so the reachable case
// never starts a reader at all.
const pasteGrace = 15 * time.Second

type deadlineReader interface {
	SetReadDeadline(t time.Time) error
}

// callbackPaster reads a callback URL the user pastes into the terminal, for
// hosts where the browser can't reach the CLI's loopback listener.
type callbackPaster struct {
	Reader io.Reader
	Logger log.Logger
	// Grace overrides how long the loopback gets before Reader is touched; zero
	// means pasteGrace. Tests set it so they don't wait out the real one.
	Grace time.Duration
}

// Fallback adapts the paster to what the OAuth flow expects.
//
// The reader is armed only after the grace period, because a blocked read on a
// terminal cannot be cancelled — SetReadDeadline is unsupported on a character
// device — so an armed reader that outlives the sign-in would race whatever
// prompt runs next for every keystroke. Deferring it means the common case (a
// browser that reaches the listener) never touches stdin, and the paste case has
// the reader finish on its own once it consumes the line. Terminal input is
// buffered, so a user who pastes during the grace period is still read.
func (p callbackPaster) Fallback(ctx context.Context, state string) (string, error) {
	graceFor := p.Grace
	if graceFor <= 0 {
		graceFor = pasteGrace
	}

	grace := time.NewTimer(graceFor)
	defer grace.Stop()

	select {
	case <-ctx.Done():
		return "", ctx.Err() //nolint:wrapcheck // the flow reports the timeout
	case <-grace.C:
	}

	pasted := make(chan oauth.PastedCallback, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.read(state, pasted)
	}()
	defer p.stop(done)

	select {
	case <-ctx.Done():
		return "", ctx.Err() //nolint:wrapcheck // the flow reports the timeout
	case res := <-pasted:
		return res.Code, res.Err
	}
}

// read parses each line as a callback URL, delivering the first conclusive one (a
// code, or an authorization error from the provider) and nudging the user on
// anything unparseable.
func (p callbackPaster) read(state string, out chan<- oauth.PastedCallback) {
	scanner := bufio.NewScanner(p.Reader)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		res := oauth.ParsePastedCallback(line, state)
		if res.Retry {
			p.warnf("That doesn't look like the callback URL (%s). Paste the full URL from the browser's address bar.", res.Err)

			continue
		}

		out <- res

		return
	}
}

// stop unblocks the reader's pending read so it can't linger and steal keystrokes
// from later prompts, then restores blocking reads.
//
// It reports failure loudly: a reader left blocked on a shared stdin competes for
// every byte with the next prompt, which looks like dropped keystrokes rather
// than an error.
func (p callbackPaster) stop(done <-chan struct{}) {
	select {
	case <-done:
		return // already finished, nothing to unblock
	default:
	}

	dr, ok := p.Reader.(deadlineReader)
	if !ok {
		p.warnUnreliable("this reader cannot be interrupted")

		return
	}

	if err := dr.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
		// Character devices reject deadlines, so this is the terminal case.
		p.warnUnreliable(err.Error())

		return
	}
	<-done
	_ = dr.SetReadDeadline(time.Time{})
}

func (p callbackPaster) warnUnreliable(reason string) {
	p.warnf("Could not stop reading standard input (%s).", reason)
	p.warnf("Later prompts in this command may drop keystrokes — pass --workspace to skip the picker.")
}

func (p callbackPaster) warnf(format string, args ...any) {
	if p.Logger != nil {
		p.Logger.Warnf(format, args...)
	}
}
