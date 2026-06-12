//go:build unit

package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// oauthMock is a test double for the WorkOS token endpoint (/oauth2/token) and
// the monolith OIDC exchange (/oidc/token), with call counters.
type oauthMock struct {
	server *httptest.Server

	mu            sync.Mutex
	tokenCalls    int // /oauth2/token (authorization_code + refresh)
	exchangeCalls int // /oidc/token (JWT → PAT)

	jwt          string
	refreshToken string
	jwtExpiresIn int64
	pat          string
	patExpiresIn int64
	failRefresh  bool
}

func newOAuthMock() *oauthMock {
	m := &oauthMock{
		jwt:          makeJWT(time.Now().Add(time.Hour).Unix()),
		refreshToken: "refresh-1",
		jwtExpiresIn: 3600,
		pat:          "bitpat_minted",
		patExpiresIn: 3600,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.tokenCalls++
		fail := m.failRefresh
		m.mu.Unlock()
		_ = r.ParseForm()
		if r.FormValue("grant_type") == "refresh_token" && fail {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)

			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  m.jwt,
			"refresh_token": m.refreshToken,
			"expires_in":    m.jwtExpiresIn,
			"token_type":    "Bearer",
		})
	})
	mux.HandleFunc("/oidc/token", func(w http.ResponseWriter, _ *http.Request) {
		m.mu.Lock()
		m.exchangeCalls++
		m.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": m.pat,
			"token_type":   "bearer",
			"expires_in":   m.patExpiresIn,
		})
	})
	m.server = httptest.NewServer(mux)

	return m
}

func (m *oauthMock) config() Config {
	return Config{
		Issuer:            m.server.URL,
		OIDCTokenEndpoint: m.server.URL + "/oidc/token",
		ClientID:          "https://cli.example/cimd.json",
		Resource:          "https://cli.example",
	}
}

func (m *oauthMock) close() { m.server.Close() }

func (m *oauthMock) counts() (tokenCalls, exchangeCalls int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.tokenCalls, m.exchangeCalls
}

func TestEnsureFresh_NotLoggedIn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newOAuthMock()
	defer m.close()

	if _, err := m.config().EnsureFresh(context.Background()); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("expected ErrNotLoggedIn, got %v", err)
	}
}

func TestEnsureFresh_ValidPAT(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := Save(Credentials{
		PAT: "still-good", PATExpiry: time.Now().Add(time.Hour),
		JWT: "j", JWTExpiry: time.Now().Add(time.Hour),
		RefreshToken: "r", WorkspaceID: "ws",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	m := newOAuthMock()
	defer m.close()

	got, err := m.config().EnsureFresh(context.Background())
	if err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if got.PAT != "still-good" || got.WorkspaceID != "ws" {
		t.Fatalf("got %+v", got)
	}
	if tc, ec := m.counts(); tc != 0 || ec != 0 {
		t.Fatalf("valid PAT should make no HTTP calls; got token=%d exchange=%d", tc, ec)
	}
}

func TestEnsureFresh_ExpiredPAT_ValidJWT(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := Save(Credentials{
		PAT: "old-pat", PATExpiry: time.Now().Add(-time.Minute),
		JWT: "good-jwt", JWTExpiry: time.Now().Add(time.Hour),
		RefreshToken: "r", WorkspaceID: "ws",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	m := newOAuthMock()
	defer m.close()

	got, err := m.config().EnsureFresh(context.Background())
	if err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if got.PAT != "bitpat_minted" || got.WorkspaceID != "ws" {
		t.Fatalf("got %+v", got)
	}
	if tc, ec := m.counts(); tc != 0 || ec != 1 {
		t.Fatalf("expected 0 token + 1 exchange; got token=%d exchange=%d", tc, ec)
	}
	if saved, _ := Load(); saved.PAT != "bitpat_minted" {
		t.Fatalf("new PAT not persisted: %q", saved.PAT)
	}
}

func TestEnsureFresh_ExpiredPATAndJWT_RefreshesAndRotates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := Save(Credentials{
		PAT: "old", PATExpiry: time.Now().Add(-time.Hour),
		JWT: "old-jwt", JWTExpiry: time.Now().Add(-time.Minute),
		RefreshToken: "refresh-old", WorkspaceID: "ws",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	m := newOAuthMock()
	defer m.close()
	m.refreshToken = "refresh-rotated" // WorkOS rotates the refresh token

	got, err := m.config().EnsureFresh(context.Background())
	if err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if got.PAT != "bitpat_minted" {
		t.Fatalf("got %+v", got)
	}
	if tc, ec := m.counts(); tc != 1 || ec != 1 {
		t.Fatalf("expected 1 refresh + 1 exchange; got token=%d exchange=%d", tc, ec)
	}
	saved, _ := Load()
	if saved.RefreshToken != "refresh-rotated" {
		t.Fatalf("rotated refresh token not persisted: %q", saved.RefreshToken)
	}
	if saved.WorkspaceID != "ws" {
		t.Fatalf("workspace not preserved through refresh: %q", saved.WorkspaceID)
	}
}

func TestEnsureFresh_RefreshRejected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := Save(Credentials{
		PAT: "old", PATExpiry: time.Now().Add(-time.Hour),
		JWT: "old-jwt", JWTExpiry: time.Now().Add(-time.Hour),
		RefreshToken: "expired-refresh", WorkspaceID: "ws",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	m := newOAuthMock()
	defer m.close()
	m.failRefresh = true

	if _, err := m.config().EnsureFresh(context.Background()); !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("expected ErrLoginRequired, got %v", err)
	}
}

// callbackOpener returns a fake browser that completes the loopback callback
// with the given code and (optionally overridden) state.
func callbackOpener(code, overrideState string) func(string) error {
	return func(rawURL string) error {
		u, err := url.Parse(rawURL)
		if err != nil {
			return err
		}
		q := u.Query()
		state := q.Get("state")
		if overrideState != "" {
			state = overrideState
		}
		cb := q.Get("redirect_uri") + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
		resp, err := http.Get(cb) //nolint:noctx,gosec // test-controlled loopback URL
		if err != nil {
			return err
		}

		return resp.Body.Close()
	}
}

func TestLogin_HappyPath(t *testing.T) {
	m := newOAuthMock()
	defer m.close()

	creds, err := m.config().Login(context.Background(), callbackOpener("auth-code", ""), io.Discard)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if creds.PAT != "bitpat_minted" {
		t.Fatalf("PAT = %q, want bitpat_minted", creds.PAT)
	}
	if creds.RefreshToken != "refresh-1" {
		t.Fatalf("refresh token = %q, want refresh-1", creds.RefreshToken)
	}
	if !creds.IsOAuthManaged() {
		t.Fatal("login result should be OAuth-managed")
	}
	if creds.WorkspaceID != "" {
		t.Fatalf("Login must not set a workspace; got %q", creds.WorkspaceID)
	}
	if creds.PATExpiry.IsZero() || creds.JWTExpiry.IsZero() {
		t.Fatal("expiries should be set after login")
	}
}

func TestLogin_StateMismatch(t *testing.T) {
	m := newOAuthMock()
	defer m.close()

	_, err := m.config().Login(context.Background(), callbackOpener("auth-code", "WRONG-STATE"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("expected state-mismatch error, got %v", err)
	}
}

func TestLogin_GuardsMissingConfig(t *testing.T) {
	if _, err := (Config{ClientID: "x"}).Login(context.Background(), nil, io.Discard); err == nil || !strings.Contains(err.Error(), "issuer") {
		t.Fatalf("expected missing-issuer error, got %v", err)
	}
	if _, err := (Config{Issuer: "https://x"}).Login(context.Background(), nil, io.Discard); err == nil || !strings.Contains(err.Error(), "client_id") {
		t.Fatalf("expected missing-client_id error, got %v", err)
	}
}
