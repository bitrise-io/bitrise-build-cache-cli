//go:build unit

package versioncheck

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper: server returning a fixed tag.
func releaseServer(t *testing.T, tag string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"` + tag + `"}`))
	}))
}

func TestRun_firstRunPersistsCurrentVersion(t *testing.T) {
	home := t.TempDir()
	srv := releaseServer(t, "v2.8.5")
	defer srv.Close()

	var out bytes.Buffer

	res, err := Run(context.Background(), Options{
		CurrentVersion: "2.8.4",
		Home:           home,
		Out:            &out,
		Now:            time.Now(),
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
	})
	require.NoError(t, err)
	assert.Equal(t, FirstRun, res.Drift.Kind)
	assert.True(t, res.NetworkCalled)
	assert.True(t, res.Behind)
	assert.Contains(t, out.String(), "2.8.5 is available")

	// State must be persisted.
	st, err := LoadState(home)
	require.NoError(t, err)
	assert.Equal(t, "2.8.4", st.LastVersion)
	assert.False(t, st.LastNudgeAt.IsZero(), "LastNudgeAt should advance when nudge fires")
}

func TestRun_noChangeWhenVersionsMatch(t *testing.T) {
	home := t.TempDir()
	srv := releaseServer(t, "v2.8.4")
	defer srv.Close()

	var out bytes.Buffer

	res, err := Run(context.Background(), Options{
		CurrentVersion: "2.8.4",
		Home:           home,
		Out:            &out,
		Now:            time.Now(),
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
	})
	require.NoError(t, err)
	assert.Equal(t, FirstRun, res.Drift.Kind) // first time we've seen any version
	assert.False(t, res.Behind, "running matches latest, nothing to nudge about")
	assert.Empty(t, out.String())
}

func TestRun_secondInvocationDetectsBump(t *testing.T) {
	home := t.TempDir()
	srv := releaseServer(t, "v2.8.5")
	defer srv.Close()

	// First invocation seeds state at 2.8.4.
	_, err := Run(context.Background(), Options{
		CurrentVersion: "2.8.4",
		Home:           home,
		Out:            &bytes.Buffer{},
		Now:            time.Now(),
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
	})
	require.NoError(t, err)

	// Second invocation with a different running binary should report Bump,
	// even with the cooldown active.
	res, err := Run(context.Background(), Options{
		CurrentVersion: "2.8.5",
		Home:           home,
		Out:            &bytes.Buffer{},
		Now:            time.Now(),
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
		NoUpdateCheck:  true, // suppress network so we don't depend on cooldown
	})
	require.NoError(t, err)
	assert.Equal(t, Bump, res.Drift.Kind)
	assert.Equal(t, "2.8.4", res.Drift.PreviousVersion)
	assert.Equal(t, "2.8.5", res.Drift.CurrentVersion)
}

func TestRun_noUpdateCheckSkipsNetwork(t *testing.T) {
	home := t.TempDir()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9"}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	res, err := Run(context.Background(), Options{
		CurrentVersion: "2.8.4",
		Home:           home,
		NoUpdateCheck:  true,
		Out:            &out,
		Now:            time.Now(),
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
	})
	require.NoError(t, err)
	assert.False(t, res.NetworkCalled)
	assert.Equal(t, 0, calls, "server must not be hit when --no-update-check is on")
	assert.Empty(t, out.String())
}

func TestRun_ciSkipsNetwork(t *testing.T) {
	home := t.TempDir()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
	}))
	defer srv.Close()

	_, err := Run(context.Background(), Options{
		CurrentVersion: "2.8.4",
		Home:           home,
		IsCI:           true,
		Out:            &bytes.Buffer{},
		Now:            time.Now(),
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, calls)
}

func TestRun_cooldownSuppressesSecondNetworkCall(t *testing.T) {
	home := t.TempDir()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"tag_name":"v2.8.5"}`))
	}))
	defer srv.Close()

	now := time.Now()

	// First run: hits network, sets LastNudgeAt.
	_, err := Run(context.Background(), Options{
		CurrentVersion: "2.8.4",
		Home:           home,
		Out:            &bytes.Buffer{},
		Now:            now,
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
	})
	require.NoError(t, err)
	require.Equal(t, 1, calls)

	// Second run, 1 hour later — must NOT call network.
	_, err = Run(context.Background(), Options{
		CurrentVersion: "2.8.4",
		Home:           home,
		Out:            &bytes.Buffer{},
		Now:            now.Add(1 * time.Hour),
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, calls, "second run inside cooldown must not call GitHub")

	// Third run, well past cooldown — calls again.
	_, err = Run(context.Background(), Options{
		CurrentVersion: "2.8.4",
		Home:           home,
		Out:            &bytes.Buffer{},
		Now:            now.Add(NudgeCooldown + time.Minute),
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "post-cooldown run should call GitHub")
}

func TestRun_networkErrorDoesNotFailRun(t *testing.T) {
	home := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := Run(context.Background(), Options{
		CurrentVersion: "2.8.4",
		Home:           home,
		Out:            &bytes.Buffer{},
		Now:            time.Now(),
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
	})
	// The caller treats errors as advisory, but Run returns them. The key
	// guarantee: state was still persisted, and LastNudgeAt was NOT advanced
	// so we retry next run.
	require.Error(t, err)

	st, loadErr := LoadState(home)
	require.NoError(t, loadErr)
	assert.Equal(t, "2.8.4", st.LastVersion)
	assert.True(t, st.LastNudgeAt.IsZero(), "network error must NOT advance LastNudgeAt; we should retry")
}
