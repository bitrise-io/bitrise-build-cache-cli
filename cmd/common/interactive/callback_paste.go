package interactive

import (
	"bufio"
	"context"
	"io"
	"sync/atomic"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/oauth"
)

// pasteGrace is how long the loopback gets before stdin is touched. A browser
// that can reach the listener does so in well under this, so the reachable case
// never starts a reader at all.
const pasteGrace = 15 * time.Second

type deadlineReader interface {
	SetReadDeadline(t time.Time) error
}

// callbackPaster covers the hosts where the browser can't reach the CLI's
// loopback listener (remote/RDE sessions).
type callbackPaster struct {
	Reader io.Reader
	Logger log.Logger
	// Grace overrides how long the loopback gets before Reader is touched; zero
	// means pasteGrace. Tests set it so they don't wait out the real one.
	Grace time.Duration
	// WorkspaceFlag is the flag the calling command accepts to skip its prompt;
	// empty means the caller has none to offer.
	WorkspaceFlag string

	// unusable records that the reader was armed and could not be stopped, so it
	// still holds stdin. Every terminal lands here — Go treats character devices
	// as non-pollable, so SetReadDeadline is rejected — which makes this the
	// normal outcome once the grace period elapses, not an edge case.
	unusable atomic.Bool
	// armed gates stop() so a reader that never entered its blocking Scan can't
	// be reported as still holding stdin. Set by read() before the first Scan.
	armed atomic.Bool
}

// StdinUnusable reports whether a reader is still holding stdin. Callers must not
// prompt after that: the two reads compete for every keystroke.
func (p *callbackPaster) StdinUnusable() bool { return p.unusable.Load() }

// The reader is armed only after the grace period, because a blocked read on a
// terminal cannot be cancelled — SetReadDeadline is unsupported on a character
// device — so an armed reader that outlives the sign-in would race whatever
// prompt runs next for every keystroke. Deferring it means the common case (a
// browser that reaches the listener) never touches stdin, and the paste case has
// the reader finish on its own once it consumes the line. Terminal input is
// buffered, so a user who pastes during the grace period is still read.
func (p *callbackPaster) Fallback(ctx context.Context, state string) (string, error) {
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

func (p *callbackPaster) read(state string, out chan<- oauth.PastedCallback) {
	p.armed.Store(true)
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

// stop unblocks the reader's pending read, then restores blocking reads. Failure
// is reported loudly: a reader left blocked on a shared stdin competes for every
// byte with the next prompt, which looks like dropped keystrokes, not an error.
func (p *callbackPaster) stop(done <-chan struct{}) {
	select {
	case <-done:
		return // already finished, nothing to unblock
	default:
	}

	if !p.armed.Load() {
		return // never entered the blocking Scan, so stdin is still free
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

// warnUnreliable marks stdin as spoken for and says so. The caller is expected to
// check StdinUnusable and stop prompting; the warning is for the case where it
// doesn't, and for the log.
func (p *callbackPaster) warnUnreliable(reason string) {
	p.unusable.Store(true)

	p.warnf("Could not stop reading standard input (%s).", reason)
	if p.WorkspaceFlag == "" {
		p.warnf("Prompts in this command are no longer reliable, so it will stop rather than drop keystrokes.")

		return
	}

	p.warnf("Prompts in this command are no longer reliable — rerun with %s to skip them.", p.WorkspaceFlag)
}

func (p *callbackPaster) warnf(format string, args ...any) {
	if p.Logger != nil {
		p.Logger.Warnf(format, args...)
	}
}
