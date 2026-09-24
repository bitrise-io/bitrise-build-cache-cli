//go:build unit

package buildhub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

// Both halves or neither: a half-set pair means the runner is not offering to
// broker, and the CLI must fall through rather than fail on a confusing request.
func TestFromEnv_NeedsBothHalves(t *testing.T) {
	_, ok := FromEnv(map[string]string{})
	assert.False(t, ok)

	_, ok = FromEnv(map[string]string{auth.EnvBuildHubVMToken: "vm-token"})
	assert.False(t, ok)

	_, ok = FromEnv(map[string]string{auth.EnvBuildHubVMTokenURL: "https://example.com"})
	assert.False(t, ok)

	_, ok = FromEnv(map[string]string{
		auth.EnvBuildHubVMToken:    "vm-token",
		auth.EnvBuildHubVMTokenURL: "https://example.com",
	})
	assert.True(t, ok)
}

func brokerServer(t *testing.T, expiresAt time.Time, calls *int32) *httptest.Server {
	t.Helper()
	var mu sync.Mutex

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*calls++
		mu.Unlock()

		assert.Equal(t, "Bearer vm-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"brokered-jwt","expiresAt":"` +
			expiresAt.Format(time.RFC3339) + `","serviceUrl":"grpcs://example"}`))
	}))
}

func clientFor(t *testing.T, url string) *Client {
	t.Helper()
	c, ok := FromEnv(map[string]string{
		auth.EnvBuildHubVMToken:    "vm-token",
		auth.EnvBuildHubVMTokenURL: url,
	})
	require.True(t, ok)

	return c
}

// The issuer's own expiry is what callers refresh against, so it must survive the
// exchange rather than being replaced by a local guess.
func TestToken_ReturnsTheIssuersExpiry(t *testing.T) {
	var calls int32
	expiresAt := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	srv := brokerServer(t, expiresAt, &calls)
	defer srv.Close()

	token, gotExpiry, err := clientFor(t, srv.URL).Token(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "brokered-jwt", token)
	assert.True(t, gotExpiry.Equal(expiresAt), "got %s, want %s", gotExpiry, expiresAt)
}

// The per-RPC path resolves on every call, so a cached token must be reused rather
// than exchanged each time.
func TestToken_CachesUntilNearExpiry(t *testing.T) {
	var calls int32
	srv := brokerServer(t, time.Now().Add(time.Hour), &calls)
	defer srv.Close()
	c := clientFor(t, srv.URL)

	for range 5 {
		_, _, err := c.Token(context.Background())
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), calls, "one exchange should serve all five resolves")
}

// A token inside the refresh skew is treated as stale, so a build step never starts
// with one about to expire underneath it.
func TestToken_ReExchangesWithinTheRefreshSkew(t *testing.T) {
	var calls int32
	srv := brokerServer(t, time.Now().Add(time.Minute), &calls)
	defer srv.Close()
	c := clientFor(t, srv.URL)

	_, _, err := c.Token(context.Background())
	require.NoError(t, err)
	_, _, err = c.Token(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int32(2), calls, "a token within the skew must not be reused")
}

// A burst of concurrent resolves must collapse into one exchange.
func TestToken_ConcurrentResolvesExchangeOnce(t *testing.T) {
	var calls int32
	srv := brokerServer(t, time.Now().Add(time.Hour), &calls)
	defer srv.Close()
	c := clientFor(t, srv.URL)

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := c.Token(context.Background())
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), calls)
}

func TestToken_ReportsHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	_, _, err := clientFor(t, srv.URL).Token(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestShared_ReusesOneClientPerPair(t *testing.T) {
	var calls int32
	srv := brokerServer(t, time.Now().Add(30*time.Minute), &calls)
	defer srv.Close()
	envs := map[string]string{auth.EnvBuildHubVMToken: "vm-token", auth.EnvBuildHubVMTokenURL: srv.URL}

	first, ok := Shared(envs)
	require.True(t, ok)
	second, ok := Shared(envs)
	require.True(t, ok)
	assert.Same(t, first, second)

	_, _, err := first.Token(context.Background())
	require.NoError(t, err)
	_, _, err = second.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(1), calls)

	_, ok = Shared(map[string]string{auth.EnvBuildHubVMToken: "vm-token"})
	assert.False(t, ok)
}
