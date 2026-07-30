//go:build unit

package oauth

import (
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

func newTestLogger(out io.Writer) log.Logger {
	return log.NewLogger(log.WithOutput(out))
}

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

// The reachable-browser case must never touch stdin: a terminal read cannot be
// cancelled, so a reader armed here would outlive Login and race the workspace
// picker for every keystroke.
func TestLogin_LoopbackWinsWithoutTouchingStdin(t *testing.T) {
	m := newOAuthMock()
	defer m.close()

	reader := newBlockingReader(os.ErrNoDeadline)
	cfg := m.config()
	cfg.PasteReader = reader

	creds, err := cfg.Login(t.Context(), callbackOpener("auth-code", ""))
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if creds.PAT != "bitpat_minted" {
		t.Fatalf("PAT = %q, want bitpat_minted", creds.PAT)
	}
	if n := reader.reads.Load(); n != 0 {
		t.Fatalf("stdin was read %d time(s) although the loopback answered", n)
	}
}

// A reader that rejects deadlines (every terminal: SetReadDeadline returns
// "file type does not support deadline" on a character device) cannot be
// stopped, so that has to be reported rather than silently ignored.
func TestStopPasteReader_WarnsWhenItCannotStop(t *testing.T) {
	var out strings.Builder

	cfg := Config{PasteReader: newBlockingReader(os.ErrNoDeadline), Logger: newTestLogger(&out)}
	cfg.stopPasteReader(make(chan struct{})) // never closed: the reader is still blocked

	if !strings.Contains(out.String(), "Could not stop reading standard input") {
		t.Fatalf("expected a warning about the unstoppable reader, got: %q", out.String())
	}
	if !strings.Contains(out.String(), "--workspace") {
		t.Fatalf("expected the warning to offer a way around it, got: %q", out.String())
	}
}

// A reader that has already finished needs no interruption and must not warn.
func TestStopPasteReader_QuietWhenReaderAlreadyDone(t *testing.T) {
	var out strings.Builder

	cfg := Config{PasteReader: newBlockingReader(os.ErrNoDeadline), Logger: newTestLogger(&out)}
	done := make(chan struct{})
	close(done)
	cfg.stopPasteReader(done)

	if out.Len() != 0 {
		t.Fatalf("expected no warning for a finished reader, got: %q", out.String())
	}
}
