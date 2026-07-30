//go:build unit

package oauth

import (
	"context"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParsePastedCallback(t *testing.T) {
	const state = "st-1"

	cases := []struct {
		name      string
		input     string
		wantCode  string
		wantErr   string
		wantRetry bool
	}{
		{
			name:     "full callback URL",
			input:    "http://127.0.0.1:54321/callback?code=abc123&state=st-1",
			wantCode: "abc123",
		},
		{
			name:     "surrounding whitespace and quotes",
			input:    "  \"http://127.0.0.1:1/callback?code=abc123&state=st-1\"  ",
			wantCode: "abc123",
		},
		{
			name:     "bare query string",
			input:    "?code=abc123&state=st-1",
			wantCode: "abc123",
		},
		{
			name:     "params only",
			input:    "code=abc123&state=st-1",
			wantCode: "abc123",
		},
		{
			name:    "state mismatch is final",
			input:   "http://127.0.0.1:1/callback?code=abc123&state=other",
			wantErr: "state mismatch",
		},
		{
			name:    "provider error is final",
			input:   "http://127.0.0.1:1/callback?error=access_denied&error_description=nope&state=st-1",
			wantErr: "authorization denied",
		},
		{
			name:      "unrelated URL asks again",
			input:     "https://app.bitrise.io/dashboard",
			wantErr:   "no code or error",
			wantRetry: true,
		},
		{
			name:      "empty input asks again",
			input:     "   ",
			wantErr:   "empty input",
			wantRetry: true,
		},
		{
			name:      "malformed query asks again",
			input:     "?code=%zz",
			wantErr:   "parse pasted callback URL",
			wantRetry: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, retry := parsePastedCallback(tc.input, state)

			if retry != tc.wantRetry {
				t.Fatalf("retry = %v, want %v (err: %v)", retry, tc.wantRetry, res.err)
			}
			if tc.wantErr == "" {
				if res.err != nil {
					t.Fatalf("unexpected error: %v", res.err)
				}
				if res.code != tc.wantCode {
					t.Fatalf("code = %q, want %q", res.code, tc.wantCode)
				}

				return
			}
			if res.err == nil || !strings.Contains(res.err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", res.err, tc.wantErr)
			}
		})
	}
}

// TestLogin_PastedCallback covers the RDE case: the browser never reaches the
// loopback listener, so the user pastes the callback URL into the terminal.
func TestLogin_PastedCallback(t *testing.T) {
	m := newOAuthMock()
	defer m.close()

	pr, pw := io.Pipe()
	cfg := m.config()
	cfg.PasteReader = pr
	cfg.PasteGrace = time.Millisecond

	creds, err := cfg.Login(context.Background(), pasteOpener(pw, ""))
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if creds.PAT != "bitpat_minted" {
		t.Fatalf("PAT = %q, want bitpat_minted", creds.PAT)
	}
}

func TestLogin_PastedGarbageThenURL(t *testing.T) {
	m := newOAuthMock()
	defer m.close()

	pr, pw := io.Pipe()
	cfg := m.config()
	cfg.PasteReader = pr
	cfg.PasteGrace = time.Millisecond

	creds, err := cfg.Login(context.Background(), pasteOpener(pw, "https://app.bitrise.io/dashboard\n"))
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if creds.PAT != "bitpat_minted" {
		t.Fatalf("PAT = %q, want bitpat_minted", creds.PAT)
	}
}

func TestLogin_PastedStateMismatch(t *testing.T) {
	m := newOAuthMock()
	defer m.close()

	pr, pw := io.Pipe()
	cfg := m.config()
	cfg.PasteReader = pr
	cfg.PasteGrace = time.Millisecond

	_, err := cfg.Login(context.Background(), func(rawURL string) error {
		u, parseErr := url.Parse(rawURL)
		if parseErr != nil {
			return parseErr
		}
		go func() {
			_, _ = io.WriteString(pw, u.Query().Get("redirect_uri")+"?code=abc&state=WRONG\n")
		}()

		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("expected state-mismatch error, got %v", err)
	}
}

// pasteOpener is a fake browser that can't reach the loopback listener: it feeds
// the callback URL back through the paste reader instead, optionally preceded by
// a line the user has to be nudged about.
func pasteOpener(w io.Writer, prefix string) func(string) error {
	return func(rawURL string) error {
		u, err := url.Parse(rawURL)
		if err != nil {
			return err
		}
		q := u.Query()
		pasted := q.Get("redirect_uri") + "?code=" + url.QueryEscape("auth-code") + "&state=" + url.QueryEscape(q.Get("state"))

		// Written from a goroutine: Login calls openBrowser before it starts
		// reading, and an unbuffered pipe write would deadlock.
		go func() { _, _ = io.WriteString(w, prefix+pasted+"\n") }()

		return nil
	}
}
