package oauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// callbackServer is the loopback (127.0.0.1, OS-assigned port) HTTP server the
// browser is redirected to; loopback avoids the macOS firewall prompt. It
// delivers the captured code or error over a buffered channel.
type callbackServer struct {
	listener net.Listener
	server   *http.Server
	state    string
	results  chan callbackResult
}

type callbackResult struct {
	code string
	err  error
}

// newCallbackServer binds the loopback listener; caller calls start() then close().
func newCallbackServer(ctx context.Context, state string) (*callbackServer, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("bind loopback callback server: %w", err)
	}
	cs := &callbackServer{
		listener: ln,
		state:    state,
		results:  make(chan callbackResult, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", cs.handle)
	cs.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second, //nolint:mnd
	}

	return cs, nil
}

// port returns the OS-assigned port the listener is bound to.
func (cs *callbackServer) port() int {
	return cs.listener.Addr().(*net.TCPAddr).Port //nolint:forcetypeassert // always *net.TCPAddr for a tcp listener
}

// redirectURI is the loopback URL baked into the authorize request and the
// later code→JWT exchange; both must match exactly.
func (cs *callbackServer) redirectURI() string {
	return fmt.Sprintf("http://127.0.0.1:%d/callback", cs.port())
}

func (cs *callbackServer) start() {
	go func() { _ = cs.server.Serve(cs.listener) }()
}

// wait blocks until the callback fires (delivering a code or error) or ctx is
// cancelled / times out.
func (cs *callbackServer) wait(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("timed out waiting for the browser sign-in to complete: %w", ctx.Err())
	case res := <-cs.results:
		return res.code, res.err
	}
}

func (cs *callbackServer) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second) //nolint:mnd
	defer cancel()
	_ = cs.server.Shutdown(ctx)
}

func (cs *callbackServer) handle(w http.ResponseWriter, r *http.Request) {
	cs.deliver(w, parseCallbackParams(r.URL.Query(), cs.state))
}

// parseCallbackParams validates the callback query params, shared by the
// loopback handler and the pasted-URL fallback so the two can't drift.
func parseCallbackParams(q url.Values, wantState string) callbackResult {
	if errCode := q.Get("error"); errCode != "" {
		return callbackResult{err: fmt.Errorf("authorization denied: %s", joinNonEmpty(errCode, q.Get("error_description")))}
	}
	if q.Get("state") != wantState {
		return callbackResult{err: errors.New("state mismatch on OAuth callback — possible CSRF, aborting")}
	}
	code := q.Get("code")
	if code == "" {
		return callbackResult{err: errors.New("OAuth callback missing authorization code")}
	}

	return callbackResult{code: code}
}

// deliver sends the result to wait() (non-blocking — the channel is buffered
// and we only keep the first result) and renders a browser-facing page.
func (cs *callbackServer) deliver(w http.ResponseWriter, res callbackResult) {
	select {
	case cs.results <- res:
	default:
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if res.err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, failurePage)

		return
	}
	_, _ = io.WriteString(w, successPage)
}

// PastedCallback is the outcome of parsing one line of user input as a callback
// URL. Retry means the line was unusable — bad paste, wrong line — as opposed to
// a sign-in the provider rejected, so the caller can ask for another line instead
// of failing the login.
type PastedCallback struct {
	Code  string
	Err   error
	Retry bool
}

// ParsePastedCallback accepts what a user can realistically copy out of the
// browser when the loopback redirect couldn't be reached: the full callback URL,
// a bare `?a=b&c=d` query string, or just the params. It applies the same state
// and error checks as the loopback handler, so a CallbackFallback cannot skip
// them.
func ParsePastedCallback(input, wantState string) PastedCallback {
	s := strings.Trim(strings.TrimSpace(input), `"'`)
	if s == "" {
		return PastedCallback{Err: errors.New("empty input"), Retry: true}
	}

	if i := strings.Index(s, "?"); i >= 0 {
		s = s[i+1:]
	}

	q, err := url.ParseQuery(s)
	if err != nil {
		return PastedCallback{Err: fmt.Errorf("parse pasted callback URL: %w", err), Retry: true}
	}
	if q.Get("code") == "" && q.Get("error") == "" {
		return PastedCallback{Err: errors.New("no code or error parameter in it"), Retry: true}
	}

	res := parseCallbackParams(q, wantState)

	return PastedCallback{Code: res.code, Err: res.err}
}

func joinNonEmpty(a, b string) string {
	if b == "" {
		return a
	}

	return a + ": " + b
}

const successPage = `<!doctype html><html><head><meta charset="utf-8"><title>Bitrise Build Cache CLI</title></head>
<body style="font-family:system-ui,sans-serif;text-align:center;padding:3rem">
<h2>✓ Signed in</h2><p>You can close this tab and return to the terminal.</p></body></html>`

const failurePage = `<!doctype html><html><head><meta charset="utf-8"><title>Bitrise Build Cache CLI</title></head>
<body style="font-family:system-ui,sans-serif;text-align:center;padding:3rem">
<h2>✗ Sign-in failed</h2><p>Return to the terminal for details.</p></body></html>`
