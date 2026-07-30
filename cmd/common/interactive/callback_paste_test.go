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

	paster := callbackPaster{Reader: pr, Grace: time.Millisecond}
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

	paster := callbackPaster{Reader: pr, Grace: time.Millisecond, Logger: log.NewLogger(log.WithOutput(&out))}
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

	paster := callbackPaster{Reader: pr, Grace: time.Millisecond}
	_, err := paster.Fallback(t.Context(), "st-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "state mismatch")
}

// The grace period is the whole reason a reachable browser never has its
// keystrokes stolen: stdin must not be touched before it elapses.
func TestCallbackPaster_DoesNotTouchTheReaderDuringTheGrace(t *testing.T) {
	reader := newBlockingReader(os.ErrNoDeadline)

	ctx, cancel := context.WithCancel(context.Background())
	paster := callbackPaster{Reader: reader, Grace: time.Hour}

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := paster.Fallback(ctx, "st-1")

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int64(0), reader.reads.Load(), "stdin was read before the grace elapsed")
}

// A reader that rejects deadlines (every terminal: SetReadDeadline returns
// "file type does not support deadline" on a character device) cannot be
// stopped, so that has to be reported rather than silently ignored.
func TestCallbackPaster_WarnsWhenItCannotStopTheReader(t *testing.T) {
	var out strings.Builder

	paster := callbackPaster{Reader: newBlockingReader(os.ErrNoDeadline), Logger: log.NewLogger(log.WithOutput(&out))}
	paster.stop(make(chan struct{})) // never closed: the reader is still blocked

	assert.Contains(t, out.String(), "Could not stop reading standard input")
	assert.Contains(t, out.String(), "--workspace", "the warning should offer a way around it")
}

func TestCallbackPaster_QuietWhenReaderAlreadyDone(t *testing.T) {
	var out strings.Builder

	paster := callbackPaster{Reader: newBlockingReader(os.ErrNoDeadline), Logger: log.NewLogger(log.WithOutput(&out))}
	done := make(chan struct{})
	close(done)
	paster.stop(done)

	assert.Empty(t, out.String(), "a finished reader needs no interruption")
}
