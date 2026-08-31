//go:build unit

package interactive

import (
	"context"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingReader blocks in Read until released, and records whether it was ever
// read from — that is what tells us if a stdin reader got armed.
type blockingReader struct {
	reads    atomic.Int64
	release  chan struct{}
	deadline error
}

func newBlockingReader(deadlineErr error) *blockingReader {
	return &blockingReader{release: make(chan struct{}), deadline: deadlineErr}
}

func (b *blockingReader) Read(_ []byte) (int, error) {
	b.reads.Add(1)
	<-b.release

	return 0, io.EOF
}

func (b *blockingReader) SetReadDeadline(time.Time) error {
	if b.deadline != nil {
		return b.deadline
	}
	close(b.release)

	return nil
}

func TestCallbackPaster_ReturnsTheCodeFromAPastedURL(t *testing.T) {
	pr, pw := io.Pipe()
	go func() {
		_, _ = io.WriteString(pw, "http://127.0.0.1:1/callback?code=auth-code&state=st-1\n")
	}()

	paster := &callbackPaster{Reader: pr, Grace: time.Millisecond}
	code, err := paster.Fallback(t.Context(), "st-1")

	require.NoError(t, err)
	assert.Equal(t, "auth-code", code)
}

// An unusable line is a mis-paste, not a failed sign-in: the user gets told what
// to paste and the reader keeps going.
func TestCallbackPaster_NudgesOnGarbageThenAcceptsTheURL(t *testing.T) {
	var out strings.Builder

	pr, pw := io.Pipe()
	go func() {
		_, _ = io.WriteString(pw, "https://app.bitrise.io/dashboard\n")
		_, _ = io.WriteString(pw, "http://127.0.0.1:1/callback?code=auth-code&state=st-1\n")
	}()

	paster := &callbackPaster{Reader: pr, Grace: time.Millisecond, Logger: log.NewLogger(log.WithOutput(&out))}
	code, err := paster.Fallback(t.Context(), "st-1")

	require.NoError(t, err)
	assert.Equal(t, "auth-code", code)
	assert.Contains(t, out.String(), "address bar")
}

// The state check has to reach the caller as a real failure, not a re-prompt.
func TestCallbackPaster_StateMismatchIsFinal(t *testing.T) {
	pr, pw := io.Pipe()
	go func() {
		_, _ = io.WriteString(pw, "http://127.0.0.1:1/callback?code=abc&state=WRONG\n")
	}()

	paster := &callbackPaster{Reader: pr, Grace: time.Millisecond}
	_, err := paster.Fallback(t.Context(), "st-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "state mismatch")
}

// The grace period is the whole reason a reachable browser never has its
// keystrokes stolen: stdin must not be touched before it elapses.
func TestCallbackPaster_DoesNotTouchTheReaderDuringTheGrace(t *testing.T) {
	reader := newBlockingReader(os.ErrNoDeadline)

	ctx, cancel := context.WithCancel(context.Background())
	paster := &callbackPaster{Reader: reader, Grace: time.Hour}

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := paster.Fallback(ctx, "st-1")

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int64(0), reader.reads.Load(), "stdin was read before the grace elapsed")
}

// Every terminal lands here — Go treats character devices as non-pollable, so
// SetReadDeadline is rejected — which is why the state has to be reported to the
// caller and not just logged.
func TestCallbackPaster_MarksStdinUnusableWhenItCannotStopTheReader(t *testing.T) {
	var out strings.Builder

	paster := &callbackPaster{
		Reader:        newBlockingReader(os.ErrNoDeadline),
		Logger:        log.NewLogger(log.WithOutput(&out)),
		WorkspaceFlag: "--workspace",
	}
	require.False(t, paster.StdinUnusable(), "nothing has been armed yet")

	paster.armed.Store(true)
	paster.stop(make(chan struct{})) // never closed: the reader is still blocked

	assert.True(t, paster.StdinUnusable(), "the caller has to be able to see this, not just read the log")
	assert.Contains(t, out.String(), "Could not stop reading standard input")
	assert.Contains(t, out.String(), "--workspace")
}

// `activate --interactive` used to be told to pass a flag it doesn't have.
func TestCallbackPaster_OmitsTheFlagAdviceWhenTheCallerHasNoFlag(t *testing.T) {
	var out strings.Builder

	paster := &callbackPaster{
		Reader: newBlockingReader(os.ErrNoDeadline),
		Logger: log.NewLogger(log.WithOutput(&out)),
	}
	paster.armed.Store(true)
	paster.stop(make(chan struct{}))

	assert.True(t, paster.StdinUnusable())
	assert.NotContains(t, out.String(), "--workspace", "the caller can't offer that flag")
	assert.Contains(t, out.String(), "stop rather than drop keystrokes")
}

// A reachable browser cancels the flow before the grace elapses, so read() never
// arms. stop() must not then claim stdin is spoken for — that mis-read is why the
// wizard used to short-circuit its own workspace picker after a first-run sign-in.
func TestCallbackPaster_ReaderNotArmedDoesNotFlagUnusable(t *testing.T) {
	var out strings.Builder

	paster := &callbackPaster{
		Reader:        newBlockingReader(os.ErrNoDeadline),
		Logger:        log.NewLogger(log.WithOutput(&out)),
		WorkspaceFlag: "--workspace",
	}
	paster.stop(make(chan struct{})) // never closed: reader was never armed

	assert.False(t, paster.StdinUnusable(), "an un-armed reader hasn't taken stdin")
	assert.Empty(t, out.String(), "an un-armed reader must not produce a warning")
}

func TestCallbackPaster_QuietWhenReaderAlreadyDone(t *testing.T) {
	var out strings.Builder

	paster := &callbackPaster{Reader: newBlockingReader(os.ErrNoDeadline), Logger: log.NewLogger(log.WithOutput(&out))}
	done := make(chan struct{})
	close(done)
	paster.stop(done)

	assert.Empty(t, out.String(), "a finished reader needs no interruption")
}

// The scenario from review: a sign-in slow enough to arm the reader (SSO + MFA
// routinely exceeds the grace), on a reader that behaves like a real terminal and
// so cannot be stopped. Before, the workspace picker ran next and dropped
// keystrokes; now the state reaches the caller.
func TestCallbackPaster_SlowSignInOnATerminalReaderReportsUnusableStdin(t *testing.T) {
	var out strings.Builder

	// os.ErrNoDeadline is what every tty returns, verified under a pty.
	reader := newBlockingReader(os.ErrNoDeadline)
	paster := &callbackPaster{
		Reader:        reader,
		Logger:        log.NewLogger(log.WithOutput(&out)),
		Grace:         time.Millisecond,
		WorkspaceFlag: "--workspace",
	}

	// The loopback wins, but only after the grace has already armed the reader.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	_, err := paster.Fallback(ctx, "st-1")

	require.ErrorIs(t, err, context.Canceled)
	assert.Positive(t, reader.reads.Load(), "the grace elapsed, so the reader must have been armed")
	assert.True(t, paster.StdinUnusable(), "an armed reader that can't be stopped still holds stdin")
}
