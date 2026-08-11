//go:build unit

package interactive

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() log.Logger { return log.NewLogger(log.WithOutput(io.Discard)) }

func TestParseLoopbackCallback_rejects(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"not loopback", "http://evil.example/callback?code=c&state=s"},
		{"https", "https://127.0.0.1:5000/callback?code=c&state=s"},
		{"no port", "http://127.0.0.1/callback?code=c&state=s"},
		{"wrong path", "http://127.0.0.1:5000/other?code=c&state=s"},
		{"no code", "http://127.0.0.1:5000/callback?state=s"},
		{"the authorize URL by mistake", "https://oauth.bitrise.io/oauth2/authorize?client_id=x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseLoopbackCallback(tc.raw)
			require.Error(t, err)
		})
	}
}

func TestParseLoopbackCallback_accepts(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1:5000/callback?code=c&state=s",
		"  'http://localhost:5000/callback?code=c&state=s'  ",
		"http://[::1]:5000/callback?error=access_denied",
	} {
		got, err := parseLoopbackCallback(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, "/callback", got.Path)
	}
}

func TestRelayCallback_deliversAndReportsRejection(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		if r.URL.Query().Get("state") != "good-state" {
			w.WriteHeader(http.StatusBadRequest)

			return
		}
	}))
	defer srv.Close()

	require.NoError(t, relayCallback(context.Background(), testLogger(), srv.URL+"/callback?code=c&state=good-state"))
	assert.Equal(t, "code=c&state=good-state", got)

	err := relayCallback(context.Background(), testLogger(), srv.URL+"/callback?code=c&state=stale")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rejected this callback")
}

// Nothing listening is the common case once a sign-in has timed out.
func TestRelayCallback_noWaitingLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	err := relayCallback(context.Background(), testLogger(), url+"/callback?code=c&state=s")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stopped waiting")
}
