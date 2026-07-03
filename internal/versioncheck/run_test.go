//go:build unit

package versioncheck

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loggerWithBuffer builds a project logger that writes into the supplied
// buffer — the standard test seam for asserting on log output from the
// versioncheck Run helper.
func loggerWithBuffer(buf *bytes.Buffer) log.Logger {
	return log.NewLogger(log.WithOutput(buf))
}

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
		Logger:         loggerWithBuffer(&out),
		Now:            time.Now(),
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
	})
	require.NoError(t, err)
	assert.Equal(t, FirstRun, res.Drift.Kind)
	assert.True(t, res.NetworkCalled)
	assert.True(t, res.Behind)
	assert.Contains(t, out.String(), "2.8.5 is available")

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
		Logger:         loggerWithBuffer(&out),
		Now:            time.Now(),
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
	})
	require.NoError(t, err)
	assert.Equal(t, FirstRun, res.Drift.Kind)
	assert.False(t, res.Behind, "running matches latest, nothing to nudge about")
	assert.Empty(t, out.String())
}

func TestRun_secondInvocationDetectsBump(t *testing.T) {
	home := t.TempDir()
	srv := releaseServer(t, "v2.8.5")
	defer srv.Close()

	_, err := Run(context.Background(), Options{
		CurrentVersion: "2.8.4",
		Home:           home,
		Logger:         loggerWithBuffer(&bytes.Buffer{}),
		Now:            time.Now(),
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
	})
	require.NoError(t, err)

	res, err := Run(context.Background(), Options{
		CurrentVersion: "2.8.5",
		Home:           home,
		Logger:         loggerWithBuffer(&bytes.Buffer{}),
		Now:            time.Now(),
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
		NoUpdateCheck:  true,
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
		Logger:         loggerWithBuffer(&out),
		Now:            time.Now(),
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
	})
	require.NoError(t, err)
	assert.False(t, res.NetworkCalled)
	assert.Equal(t, 0, calls, "server must not be hit when --no-update-check is on")
	assert.Empty(t, out.String())
}

// TestRun_noChangeWithSuppressedNudgeSkipsSaveState locks the hot-path
// optimisation: when the running version matches the persisted version AND
// the nudge is suppressed (CI / cooldown / --no-update-check), Run skips
// the mkdir + temp-file + atomic rename SaveState does. The state file's
// modification time must not advance.
func TestRun_noChangeWithSuppressedNudgeSkipsSaveState(t *testing.T) {
	home := t.TempDir()
	now := time.Now()

	require.NoError(t, SaveState(home, State{
		LastVersion: "2.8.4",
		LastSeenAt:  now.Add(-1 * time.Hour),
	}))

	statePath := filepath.Join(home, paths.StateDirRelative, StateFile)
	infoBefore, err := os.Stat(statePath)
	require.NoError(t, err)

	_, err = Run(context.Background(), Options{
		CurrentVersion: "2.8.4",
		Home:           home,
		IsCI:           true,
		Logger:         loggerWithBuffer(&bytes.Buffer{}),
		Now:            now,
	})
	require.NoError(t, err)

	infoAfter, err := os.Stat(statePath)
	require.NoError(t, err)
	assert.Equal(t, infoBefore.ModTime(), infoAfter.ModTime(),
		"state file MUST NOT be rewritten when drift is NoChange and nudge is suppressed")
}

func TestRun_bumpStillSavesStateEvenWithSuppressedNudge(t *testing.T) {
	home := t.TempDir()
	now := time.Now()

	require.NoError(t, SaveState(home, State{
		LastVersion: "2.8.3",
		LastSeenAt:  now.Add(-1 * time.Hour),
	}))

	_, err := Run(context.Background(), Options{
		CurrentVersion: "2.8.4",
		Home:           home,
		IsCI:           true,
		Logger:         loggerWithBuffer(&bytes.Buffer{}),
		Now:            now,
	})
	require.NoError(t, err)

	st, err := LoadState(home)
	require.NoError(t, err)
	assert.Equal(t, "2.8.4", st.LastVersion)
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
		Logger:         loggerWithBuffer(&bytes.Buffer{}),
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

	_, err := Run(context.Background(), Options{
		CurrentVersion: "2.8.4",
		Home:           home,
		Logger:         loggerWithBuffer(&bytes.Buffer{}),
		Now:            now,
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
	})
	require.NoError(t, err)
	require.Equal(t, 1, calls)

	_, err = Run(context.Background(), Options{
		CurrentVersion: "2.8.4",
		Home:           home,
		Logger:         loggerWithBuffer(&bytes.Buffer{}),
		Now:            now.Add(1 * time.Hour),
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, calls, "second run inside cooldown must not call GitHub")

	_, err = Run(context.Background(), Options{
		CurrentVersion: "2.8.4",
		Home:           home,
		Logger:         loggerWithBuffer(&bytes.Buffer{}),
		Now:            now.Add(NudgeCooldown + time.Minute),
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "post-cooldown run should call GitHub")
}

func TestRun_throttleResponseAdvancesLastNudgeAt(t *testing.T) {
	home := t.TempDir()
	now := time.Now()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := Run(context.Background(), Options{
		CurrentVersion: "2.8.4",
		Home:           home,
		Logger:         loggerWithBuffer(&bytes.Buffer{}),
		Now:            now,
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
	})
	require.Error(t, err, "throttle is still surfaced as an error to the caller")

	st, loadErr := LoadState(home)
	require.NoError(t, loadErr)
	assert.Equal(t, now.UTC(), st.LastNudgeAt.UTC(),
		"403/429 MUST advance LastNudgeAt to throttle subsequent runs")
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
		Logger:         loggerWithBuffer(&bytes.Buffer{}),
		Now:            time.Now(),
		HTTPClient:     srv.Client(),
		FetchURL:       srv.URL,
	})
	require.Error(t, err)

	st, loadErr := LoadState(home)
	require.NoError(t, loadErr)
	assert.Equal(t, "2.8.4", st.LastVersion)
	assert.True(t, st.LastNudgeAt.IsZero(), "network error must NOT advance LastNudgeAt; we should retry")
}
