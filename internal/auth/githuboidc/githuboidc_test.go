//go:build unit

package githuboidc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

const testSub = "repo:acme@1/app@2:ref:refs/heads/main"

// githubToken is shaped like a GitHub Actions OIDC token; n makes each one unique,
// as GitHub's are.
func githubToken(t *testing.T, n int32) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"sub": testSub, "repository": "acme/app", "ref": "refs/heads/main",
		"event_name": "push", "jti": fmt.Sprint(n),
	})
	require.NoError(t, err)

	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

// fakes stands in for GitHub's token endpoint and the Bitrise exchange.
type fakes struct {
	github   *httptest.Server
	exchange *httptest.Server

	githubCalls   atomic.Int32
	exchangeCalls atomic.Int32

	mu            sync.Mutex
	subjects      []string
	status        int
	body          string
	expiresIn     int64
	exchangeDelay time.Duration
}

func newFakes(t *testing.T) *fakes {
	t.Helper()
	f := &fakes{status: http.StatusOK, expiresIn: 900}

	f.github = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := f.githubCalls.Add(1)
		assert.Equal(t, "Bearer request-token", r.Header.Get("Authorization"))
		assert.Equal(t, "app.bitrise.io", r.URL.Query().Get("audience"))
		assert.Equal(t, "2.0", r.URL.Query().Get("api-version"), "the request URL's own query must survive")
		_ = json.NewEncoder(w).Encode(map[string]any{"count": 1, "value": githubToken(t, n)})
	}))
	t.Cleanup(f.github.Close)

	f.exchange = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := f.exchangeCalls.Add(1)
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "urn:ietf:params:oauth:grant-type:token-exchange", r.PostForm.Get("grant_type"))
		assert.Equal(t, "id_token", r.PostForm.Get("subject_token_type"))
		assert.Equal(t, "policy-uuid", r.PostForm.Get("policy_id"))

		f.mu.Lock()
		f.subjects = append(f.subjects, r.PostForm.Get("subject_token"))
		status, body, expiresIn, delay := f.status, f.body, f.expiresIn, f.exchangeDelay
		f.mu.Unlock()

		time.Sleep(delay)
		w.WriteHeader(status)
		if body != "" {
			_, _ = w.Write([]byte(body))

			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("wat-%d", n), "token_type": "bearer", "expires_in": expiresIn,
		})
	}))
	t.Cleanup(f.exchange.Close)

	return f
}

func (f *fakes) envs() map[string]string {
	return map[string]string{
		auth.EnvOIDCPolicyID:           "policy-uuid",
		auth.EnvGitHubOIDCRequestURL:   f.github.URL + "/idtoken?api-version=2.0",
		auth.EnvGitHubOIDCRequestToken: "request-token",
		auth.EnvOIDCTokenEndpoint:      f.exchange.URL,
	}
}

func (f *fakes) client(t *testing.T) (*Client, *time.Time) {
	t.Helper()
	c, ok := FromEnv(f.envs())
	require.True(t, ok)
	now := time.Now()
	c.now = func() time.Time { return now }

	return c, &now
}

// All three, or the CLI is not being asked to exchange and must fall through.
func TestFromEnv_NeedsPolicyAndGitHubToken(t *testing.T) {
	full := map[string]string{
		auth.EnvOIDCPolicyID:           "policy-uuid",
		auth.EnvGitHubOIDCRequestURL:   "https://example.com/idtoken",
		auth.EnvGitHubOIDCRequestToken: "request-token",
	}
	_, ok := FromEnv(full)
	assert.True(t, ok)

	for key := range full {
		partial := map[string]string{}
		for k, v := range full {
			if k != key {
				partial[k] = v
			}
		}
		_, ok := FromEnv(partial)
		assert.False(t, ok, "without %s", key)
	}
}

func TestFromEnv_DefaultsToTheProductionEndpoint(t *testing.T) {
	c, ok := FromEnv(map[string]string{
		auth.EnvOIDCPolicyID:           "policy-uuid",
		auth.EnvGitHubOIDCRequestURL:   "https://example.com/idtoken",
		auth.EnvGitHubOIDCRequestToken: "request-token",
	})
	require.True(t, ok)
	assert.Equal(t, auth.DefaultOIDCTokenEndpoint, c.endpoint)
}

func TestToken_ExchangesAndStampsTheServersExpiry(t *testing.T) {
	f := newFakes(t)
	c, now := f.client(t)

	token, expiresAt, err := c.Token(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "wat-1", token)
	assert.Equal(t, now.Add(900*time.Second), expiresAt)
}

// Every exchange mints a token server-side, so a burst of resolves must not.
func TestToken_ReusesTheCachedToken(t *testing.T) {
	f := newFakes(t)
	c, _ := f.client(t)

	for range 3 {
		_, _, err := c.Token(context.Background())
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), f.exchangeCalls.Load())
	assert.Equal(t, int32(1), f.githubCalls.Load())
}

// The exchange accepts each GitHub token once, so a refresh must never reuse one.
func TestToken_RefreshesNearExpiryWithAFreshGitHubToken(t *testing.T) {
	f := newFakes(t)
	c, now := f.client(t)

	first, _, err := c.Token(context.Background())
	require.NoError(t, err)

	*now = now.Add(900*time.Second - refreshSkew + time.Second)
	second, _, err := c.Token(context.Background())
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
	assert.Equal(t, int32(2), f.githubCalls.Load())
	f.mu.Lock()
	defer f.mu.Unlock()
	require.Len(t, f.subjects, 2)
	assert.NotEqual(t, f.subjects[0], f.subjects[1], "a subject token was replayed")
}

// The endpoint says only invalid_token, whatever the cause; the job's own claims
// are what the user needs to write a matching policy.
func TestToken_RejectionCarriesTheJobsClaims(t *testing.T) {
	f := newFakes(t)
	f.status, f.body = http.StatusBadRequest, `{"error":"invalid_token"}`
	c, _ := f.client(t)

	_, _, err := c.Token(context.Background())

	var rejected *RejectedError
	require.ErrorAs(t, err, &rejected)
	assert.Equal(t, "policy-uuid", rejected.PolicyID)
	assert.Equal(t, testSub, rejected.Claims.Sub)
	assert.Equal(t, "acme/app", rejected.Claims.Repository)
	assert.Equal(t, "push", rejected.Claims.EventName)
	assert.Contains(t, err.Error(), testSub)
	assert.NotContains(t, err.Error(), "header.", "the GitHub token itself must never appear")
}

func TestToken_ServerErrorIsNotARejection(t *testing.T) {
	f := newFakes(t)
	f.status, f.body = http.StatusBadGateway, "upstream down"
	c, _ := f.client(t)

	_, _, err := c.Token(context.Background())

	require.Error(t, err)
	var rejected *RejectedError
	assert.False(t, errors.As(err, &rejected))
}

// A policy that rejects this job's token will reject the next one too.
func TestToken_BacksOffAfterAFailure(t *testing.T) {
	f := newFakes(t)
	f.status, f.body = http.StatusBadRequest, `{"error":"invalid_token"}`
	c, now := f.client(t)

	_, _, err := c.Token(context.Background())
	require.Error(t, err)
	_, _, err = c.Token(context.Background())
	require.Error(t, err)
	assert.Equal(t, int32(1), f.exchangeCalls.Load(), "retried inside the back-off")

	f.mu.Lock()
	f.status, f.body = http.StatusOK, ""
	f.mu.Unlock()
	*now = now.Add(failureBackoff)

	token, _, err := c.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "wat-2", token)
}

// A failed early refresh must not cost the caller a token that still works.
func TestToken_ServesTheUnexpiredTokenWhenRefreshFails(t *testing.T) {
	f := newFakes(t)
	c, now := f.client(t)

	first, _, err := c.Token(context.Background())
	require.NoError(t, err)

	f.mu.Lock()
	f.status, f.body = http.StatusBadGateway, "upstream down"
	f.mu.Unlock()
	*now = now.Add(900*time.Second - time.Minute)

	token, _, err := c.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, first, token)
}

func TestToken_ConcurrentCallersShareOneExchange(t *testing.T) {
	f := newFakes(t)
	f.exchangeDelay = 50 * time.Millisecond
	c, _ := f.client(t)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := c.Token(context.Background())
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), f.exchangeCalls.Load())
}

func TestToken_MissingExpiryIsAnError(t *testing.T) {
	f := newFakes(t)
	f.expiresIn = 0
	c, _ := f.client(t)

	_, _, err := c.Token(context.Background())

	require.ErrorIs(t, err, errNoExpiry)
}
