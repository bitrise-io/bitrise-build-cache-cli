//go:build unit

package buildhub

import (
	"context"
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

// Both halves or neither: a half-set pair means the runner is not offering to
// broker, and the CLI must fall through rather than fail on a confusing request.
func TestFromEnv_NeedsBothHalves(t *testing.T) {
	_, ok := FromEnv(map[string]string{})
	assert.False(t, ok)

	_, ok = FromEnv(map[string]string{auth.EnvBuildHubVMToken: "vm-token"})
	assert.False(t, ok)

	_, ok = FromEnv(map[string]string{auth.EnvBuildHubVMTokenURL: "https://api.bitrise.io"})
	assert.False(t, ok)

	_, ok = FromEnv(map[string]string{
		auth.EnvBuildHubVMToken:    "vm-token",
		auth.EnvBuildHubVMTokenURL: "https://api.bitrise.io",
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

func TestToken_FailedRefreshKeepsAnUnexpiredToken(t *testing.T) {
	var calls int32
	fail := false
	var mu sync.Mutex
	expiresAt := time.Now().Add(time.Minute)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if fail {
			w.WriteHeader(http.StatusBadGateway)

			return
		}
		_, _ = w.Write([]byte(`{"accessToken":"brokered-jwt","expiresAt":"` + expiresAt.Format(time.RFC3339) + `"}`))
	}))
	defer srv.Close()
	c := clientFor(t, srv.URL)

	_, _, err := c.Token(context.Background())
	require.NoError(t, err)
	mu.Lock()
	fail = true
	mu.Unlock()

	token, _, err := c.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "brokered-jwt", token)
	assert.Equal(t, int32(2), calls)
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

const (
	bhURL  = "https://api.bitrise.io/build-hub/vm/tokens"
	rdeURL = "https://instance-manager.services.bitrise.io/vm/tokens"
)

// The first usable pair wins, a pair is usable only when both halves are set and the
// URL is trusted, and halves never combine across pairs. TokenEnv must agree with
// FromEnv, since the offline callers rely on it to predict the exchange.
func TestFromEnv_PicksTheFirstUsablePair(t *testing.T) {
	for _, tc := range []struct {
		name      string
		envs      map[string]string
		wantEnv   string
		wantToken string
		wantURL   string
	}{
		{"nothing", map[string]string{}, "", "", ""},
		{"Build Hub pair", map[string]string{auth.EnvBuildHubVMToken: "bh", auth.EnvBuildHubVMTokenURL: bhURL}, auth.EnvBuildHubVMToken, "bh", bhURL},
		{"neutral pair", map[string]string{auth.EnvVMToken: "rde", auth.EnvVMTokenURL: rdeURL}, auth.EnvVMToken, "rde", rdeURL},
		{"both pairs", map[string]string{
			auth.EnvBuildHubVMToken: "bh", auth.EnvBuildHubVMTokenURL: bhURL,
			auth.EnvVMToken: "rde", auth.EnvVMTokenURL: rdeURL,
		}, auth.EnvBuildHubVMToken, "bh", bhURL},
		{"half-set Build Hub pair", map[string]string{auth.EnvBuildHubVMToken: "bh", auth.EnvVMToken: "rde", auth.EnvVMTokenURL: rdeURL}, auth.EnvVMToken, "rde", rdeURL},
		{"untrusted Build Hub URL", map[string]string{
			auth.EnvBuildHubVMToken: "bh", auth.EnvBuildHubVMTokenURL: "https://example.com/vm/tokens",
			auth.EnvVMToken: "rde", auth.EnvVMTokenURL: rdeURL,
		}, auth.EnvVMToken, "rde", rdeURL},
		{"untrusted neutral URL", map[string]string{auth.EnvVMToken: "rde", auth.EnvVMTokenURL: "https://example.com/vm/tokens"}, "", "", ""},
		{"http neutral URL", map[string]string{auth.EnvVMToken: "rde", auth.EnvVMTokenURL: "http://instance-manager.services.bitrise.io/vm/tokens"}, "", "", ""},
		{"whitespace URL", map[string]string{auth.EnvVMToken: "rde", auth.EnvVMTokenURL: "  "}, "", "", ""},
		{"whitespace token", map[string]string{auth.EnvVMToken: " ", auth.EnvVMTokenURL: rdeURL}, "", "", ""},
		{"padded values", map[string]string{auth.EnvVMToken: " rde\n", auth.EnvVMTokenURL: " " + rdeURL + "\n"}, auth.EnvVMToken, "rde", rdeURL},
		{"halves across pairs", map[string]string{auth.EnvBuildHubVMToken: "bh", auth.EnvVMTokenURL: rdeURL}, "", "", ""},
	} {
		c, ok := FromEnv(tc.envs)
		assert.Equal(t, tc.wantEnv, TokenEnv(tc.envs), tc.name)
		if !assert.Equal(t, tc.wantEnv != "", ok, tc.name) || !ok {
			continue
		}
		assert.Equal(t, tc.wantToken, c.vmToken, tc.name)
		assert.Equal(t, tc.wantURL, c.tokenURL, tc.name)
	}
}

// A down instance-manager must not cost every per-RPC resolve another request.
func TestToken_BacksOffAfterAFailure(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	c := clientFor(t, srv.URL)

	_, _, firstErr := c.Token(context.Background())
	_, _, secondErr := c.Token(context.Background())

	require.Error(t, firstErr)
	require.ErrorIs(t, secondErr, firstErr)
	assert.Contains(t, secondErr.Error(), "backing off")
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

// hangingServer holds a request that arrives while slow is set until the request is
// cancelled; once slow is cleared, later requests get an hour-long token.
func hangingServer(t *testing.T, slow *atomic.Bool, calls *int32) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	expiresAt := time.Now().Add(time.Hour).Format(time.RFC3339)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		if slow.Load() {
			select {
			case <-r.Context().Done():
			case <-release:
			}

			return
		}
		_, _ = w.Write([]byte(`{"accessToken":"brokered-jwt","expiresAt":"` + expiresAt + `"}`))
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	return srv
}

// A caller that gave up must not put every later caller into the backoff.
func TestToken_CancelledCallerDoesNotBackOff(t *testing.T) {
	var slow atomic.Bool
	var calls int32
	slow.Store(true)
	c := clientFor(t, hangingServer(t, &slow, &calls).URL)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	_, _, err := c.Token(ctx)
	require.ErrorIs(t, err, context.Canceled)
	slow.Store(false)

	token, _, err := c.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "brokered-jwt", token)
}

// A hanging instance-manager must back off short-deadline callers too, or each of
// them waits out its whole deadline.
func TestToken_DeadlineExceededBacksOff(t *testing.T) {
	var slow atomic.Bool
	var calls int32
	slow.Store(true)
	c := clientFor(t, hangingServer(t, &slow, &calls).URL)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := c.Token(ctx)
	require.Error(t, err)
	slow.Store(false)

	_, _, err = c.Token(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backing off")
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func failingServer(t *testing.T, fail *atomic.Bool, calls *int32, expiresAt time.Time) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(calls, 1)
		if fail.Load() {
			w.WriteHeader(http.StatusBadGateway)

			return
		}
		_, _ = w.Write([]byte(`{"accessToken":"brokered-jwt","expiresAt":"` + expiresAt.Format(time.RFC3339) + `"}`))
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestToken_BackoffExpires(t *testing.T) {
	var fail atomic.Bool
	var calls int32
	fail.Store(true)
	now := time.Now()
	c := clientFor(t, failingServer(t, &fail, &calls, now.Add(time.Hour)).URL)
	c.now = func() time.Time { return now }

	_, _, err := c.Token(context.Background())
	require.Error(t, err)
	fail.Store(false)
	now = now.Add(failureBackoff - time.Second)
	_, _, err = c.Token(context.Background())
	require.Error(t, err, "still backing off")

	now = now.Add(2 * time.Second)
	token, _, err := c.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "brokered-jwt", token)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

// The backoff skips the exchange, not the token already held.
func TestToken_BackoffStillServesAnUnexpiredToken(t *testing.T) {
	var fail atomic.Bool
	var calls int32
	now := time.Now()
	c := clientFor(t, failingServer(t, &fail, &calls, now.Add(refreshSkew+time.Minute)).URL)
	c.now = func() time.Time { return now }

	_, _, err := c.Token(context.Background())
	require.NoError(t, err)
	fail.Store(true)
	now = now.Add(2 * time.Minute)

	for range 2 {
		token, _, err := c.Token(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "brokered-jwt", token)
	}
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls), "the second early refresh is backed off")
}
