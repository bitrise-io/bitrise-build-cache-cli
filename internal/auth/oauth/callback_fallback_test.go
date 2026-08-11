//go:build unit

package oauth

import (
	"context"
	"net/url"
	"strings"
	"sync/atomic"
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
			res := ParsePastedCallback(tc.input, state)

			if res.Retry != tc.wantRetry {
				t.Fatalf("retry = %v, want %v (err: %v)", res.Retry, tc.wantRetry, res.Err)
			}
			if tc.wantErr == "" {
				if res.Err != nil {
					t.Fatalf("unexpected error: %v", res.Err)
				}
				if res.Code != tc.wantCode {
					t.Fatalf("code = %q, want %q", res.Code, tc.wantCode)
				}

				return
			}
			if res.Err == nil || !strings.Contains(res.Err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", res.Err, tc.wantErr)
			}
		})
	}
}

// The flow has to take a code from the fallback when the browser never reaches
// the loopback listener — the RDE case, where localhost is not the CLI's host.
func TestLogin_FallbackSuppliesTheCode(t *testing.T) {
	m := newOAuthMock()
	defer m.close()

	cfg := m.config()
	cfg.CallbackFallback = func(_ context.Context, state string) (string, error) {
		res := ParsePastedCallback("http://127.0.0.1:1/callback?code=auth-code&state="+url.QueryEscape(state), state)

		return res.Code, res.Err
	}

	creds, err := cfg.Login(context.Background(), func(string) error { return nil })
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if creds.AuthToken != "bitpat_minted" {
		t.Fatalf("PAT = %q, want bitpat_minted", creds.AuthToken)
	}
}

// A fallback cannot bypass the state check, or a pasted URL would be a CSRF hole.
func TestLogin_FallbackStateMismatch(t *testing.T) {
	m := newOAuthMock()
	defer m.close()

	cfg := m.config()
	cfg.CallbackFallback = func(_ context.Context, state string) (string, error) {
		res := ParsePastedCallback("http://127.0.0.1:1/callback?code=abc&state=WRONG", state)

		return res.Code, res.Err
	}

	_, err := cfg.Login(context.Background(), func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("expected state-mismatch error, got %v", err)
	}
}

// When the loopback answers, the fallback must be told to stop and be waited for:
// until it returns it may still hold stdin, which the next prompt then competes
// with for keystrokes.
func TestLogin_LoopbackWinCancelsTheFallbackAndWaits(t *testing.T) {
	m := newOAuthMock()
	defer m.close()

	released := make(chan struct{})
	var cancelled atomic.Bool

	cfg := m.config()
	cfg.CallbackFallback = func(ctx context.Context, _ string) (string, error) {
		<-ctx.Done()
		// Releasing stdin isn't instant (a deadline has to be set and the pending
		// read has to unwind), so the wait has to be real, not a race won by luck.
		time.Sleep(50 * time.Millisecond)
		cancelled.Store(true)
		close(released)

		return "", ctx.Err()
	}

	if _, err := cfg.Login(context.Background(), callbackOpener("auth-code", "")); err != nil {
		t.Fatalf("Login: %v", err)
	}

	select {
	case <-released:
	default:
		t.Fatal("Login returned before the fallback finished releasing its input")
	}
	if !cancelled.Load() {
		t.Fatal("the fallback was never cancelled")
	}
}
