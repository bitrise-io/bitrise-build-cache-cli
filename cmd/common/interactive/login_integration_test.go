//go:build unit

package interactive

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/bitriseapi"
)

// fakeIdentity redirects to whatever loopback callback the CLI bound, the way the
// real provider does.
func fakeIdentity(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		redirect, err := url.Parse(q.Get("redirect_uri"))
		require.NoError(t, err)
		rq := redirect.Query()
		rq.Set("code", "auth-code")
		rq.Set("state", q.Get("state"))
		redirect.RawQuery = rq.Encode()
		http.Redirect(w, r, redirect.String(), http.StatusFound)
	})
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		payload, err := json.Marshal(map[string]any{"exp": time.Now().Add(time.Hour).Unix()})
		require.NoError(t, err)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "hdr." + base64.RawURLEncoding.EncodeToString(payload) + ".sig",
			"refresh_token": "refresh-1",
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
	})
	mux.HandleFunc("/oidc/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "bitpat_minted",
			"token_type":   "bearer",
			"expires_in":   3600,
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func fakeOrganizations(t *testing.T, workspaces ...bitriseapi.Workspace) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/organizations", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": workspaces})
	}))
	t.Cleanup(srv.Close)

	return srv
}

// TestLoginNoWorkspace_agentFlow walks the path an agent drives on a machine
// with no terminal: sign in, discover that no workspace is selected, list the
// options, pick one.
func TestLoginNoWorkspace_agentFlow(t *testing.T) {
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(auth.EnvAuthToken, "")
	t.Setenv(auth.EnvWorkspaceID, "")
	t.Setenv(auth.EnvJWT, "")

	identity := fakeIdentity(t)
	t.Setenv("BITRISE_OAUTH_ISSUER", identity.URL)
	t.Setenv("BITRISE_OIDC_TOKEN_ENDPOINT", identity.URL+"/oidc/token")
	t.Setenv("BITRISE_OAUTH_CLIENT_ID", "https://cli.example/cimd.json")

	orgs := fakeOrganizations(t,
		bitriseapi.Workspace{Slug: "ws-one", Name: "One"},
		bitriseapi.Workspace{Slug: "ws-two", Name: "Two"},
	)
	t.Setenv("BITRISE_API_BASE_URL", orgs.URL)

	// The user's browser: opens the authorize URL and follows the redirect back
	// to the CLI's loopback listener.
	browsed := make(chan error, 1)
	openBrowser = func(authorizeURL string) error {
		go func() {
			resp, err := http.Get(authorizeURL) //nolint:noctx // stands in for a browser
			if err == nil {
				_ = resp.Body.Close()
			}
			browsed <- err
		}()

		return nil
	}
	t.Cleanup(func() { openBrowser = defaultOpenBrowser })

	// `go test` inherits the developer's terminal, so pin stdin to a non-TTY. A
	// pipe, not /dev/null — that is a character device, which reads as a TTY.
	pipeR, pipeW, err := os.Pipe()
	require.NoError(t, err)
	realStdin := os.Stdin
	os.Stdin = pipeR
	t.Cleanup(func() { os.Stdin = realStdin; _ = pipeR.Close(); _ = pipeW.Close() })
	require.False(t, isInteractiveStdin())

	LoginCmd.SetArgs([]string{"--no-workspace"})
	t.Cleanup(func() { LoginCmd.SetArgs(nil); loginNoWorkspace = false })
	require.NoError(t, LoginCmd.ExecuteContext(context.Background()))
	require.NoError(t, <-browsed)

	stored, err := store.NewKeychain().Load()
	require.NoError(t, err)
	assert.Equal(t, "bitpat_minted", stored.AuthToken)
	assert.Equal(t, "refresh-1", stored.RefreshToken)
	assert.Empty(t, stored.WorkspaceID, "--no-workspace must not invent a workspace")

	envs := map[string]string{}
	resolver := live.Default(nil)
	_, _, err = resolver.Resolve(context.Background(), envs)
	require.ErrorIs(t, err, auth.ErrWorkspaceNotSelected, "the half-finished login must not resolve as usable")

	// What the agent shows the user, then what it picks for them.
	cred, _, err := resolver.ResolveTokenOnly(context.Background(), envs)
	require.NoError(t, err)
	workspaces, err := bitriseapi.ListWorkspaces(context.Background(), orgs.URL, cred.Token)
	require.NoError(t, err)
	require.Len(t, workspaces, 2)

	_, err = store.SetWorkspaceID(false, workspaces[1].Slug)
	require.NoError(t, err)

	cred, origin, err := resolver.Resolve(context.Background(), envs)
	require.NoError(t, err)
	assert.Equal(t, "ws-two", cred.WorkspaceID)
	assert.Equal(t, "bitpat_minted", cred.Token)
	assert.Equal(t, auth.ProvenanceOAuthLogin, origin.Provenance, "picking a workspace must not downgrade the login")
}

// lineSink hands whatever the command prints to stdout back to the test, without
// a buffer both goroutines would race on.
type lineSink struct{ lines chan string }

func (s lineSink) Write(p []byte) (int, error) {
	s.lines <- strings.TrimSpace(string(p))

	return len(p), nil
}

// TestLoginPrintURLAndCallback_twoCommandFlow is the flow with no terminal and no
// browser on the CLI's host: one command prints the URL and waits, the browser
// runs elsewhere, and a second command delivers the address it landed on.
func TestLoginPrintURLAndCallback_twoCommandFlow(t *testing.T) {
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(auth.EnvAuthToken, "")
	t.Setenv(auth.EnvWorkspaceID, "")
	t.Setenv(auth.EnvJWT, "")

	identity := fakeIdentity(t)
	t.Setenv("BITRISE_OAUTH_ISSUER", identity.URL)
	t.Setenv("BITRISE_OIDC_TOKEN_ENDPOINT", identity.URL+"/oidc/token")
	t.Setenv("BITRISE_OAUTH_CLIENT_ID", "https://cli.example/cimd.json")

	openBrowser = func(string) error {
		t.Error("--print-url must not open a browser")

		return nil
	}
	t.Cleanup(func() { openBrowser = defaultOpenBrowser })

	pipeR, pipeW, err := os.Pipe()
	require.NoError(t, err)
	realStdin := os.Stdin
	os.Stdin = pipeR
	t.Cleanup(func() { os.Stdin = realStdin; _ = pipeR.Close(); _ = pipeW.Close() })

	sink := lineSink{lines: make(chan string, 4)}
	LoginCmd.SetOut(sink)
	LoginCmd.SetArgs([]string{"--print-url", "--no-workspace"})
	t.Cleanup(func() {
		LoginCmd.SetArgs(nil)
		LoginCmd.SetOut(nil)
		loginPrintURL, loginNoWorkspace = false, false
	})

	done := make(chan error, 1)
	go func() { done <- LoginCmd.ExecuteContext(context.Background()) }()

	authorizeURL := <-sink.lines
	require.Contains(t, authorizeURL, identity.URL+"/oauth2/authorize")

	// The browser, on some other machine: it can reach the provider but not the
	// CLI's loopback, so it stops at the redirect the CLI must be handed.
	noRedirects := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noRedirects.Get(authorizeURL) //nolint:noctx // stands in for a browser
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	landedOn := resp.Header.Get("Location")
	require.Contains(t, landedOn, "127.0.0.1")

	require.NoError(t, relayCallback(context.Background(), testLogger(), landedOn))
	require.NoError(t, <-done)

	stored, err := store.NewKeychain().Load()
	require.NoError(t, err)
	assert.Equal(t, "bitpat_minted", stored.AuthToken)
	assert.Empty(t, stored.WorkspaceID)
}
