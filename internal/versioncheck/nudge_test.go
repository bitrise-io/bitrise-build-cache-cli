//go:build unit

package versioncheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldNudge_allowedByDefault(t *testing.T) {
	err := ShouldNudge(NudgeDecision{Now: time.Now()})
	assert.NoError(t, err)
}

func TestShouldNudge_suppressedByNoUpdateCheck(t *testing.T) {
	err := ShouldNudge(NudgeDecision{NoUpdateCheckFlag: true, Now: time.Now()})
	assert.ErrorIs(t, err, ErrNudgeSuppressed)
}

func TestShouldNudge_suppressedByCI(t *testing.T) {
	err := ShouldNudge(NudgeDecision{IsCI: true, Now: time.Now()})
	assert.ErrorIs(t, err, ErrNudgeSuppressed)
}

func TestShouldNudge_suppressedInsideCooldown(t *testing.T) {
	now := time.Now()
	err := ShouldNudge(NudgeDecision{Now: now, LastNudgeAt: now.Add(-1 * time.Hour)})
	assert.ErrorIs(t, err, ErrNudgeSuppressed, "1h ago is inside the 24h cooldown")
}

func TestShouldNudge_allowedAfterCooldown(t *testing.T) {
	now := time.Now()
	err := ShouldNudge(NudgeDecision{Now: now, LastNudgeAt: now.Add(-NudgeCooldown - time.Minute)})
	assert.NoError(t, err)
}

func TestFetchLatestVersion_stripsLeadingV(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v2.8.5"}`))
	}))
	defer server.Close()

	got, err := FetchLatestVersion(context.Background(), server.Client(), server.URL)
	require.NoError(t, err)
	assert.Equal(t, "2.8.5", got)
}

func TestFetchLatestVersion_propagatesHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := FetchLatestVersion(context.Background(), server.Client(), server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestFetchLatestVersion_returnsThrottledOn429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	_, err := FetchLatestVersion(context.Background(), server.Client(), server.URL)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrThrottled)
}

func TestFetchLatestVersion_returnsThrottledOn403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	_, err := FetchLatestVersion(context.Background(), server.Client(), server.URL)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrThrottled)
}

func TestFetchLatestVersion_emptyTagErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":""}`))
	}))
	defer server.Close()

	_, err := FetchLatestVersion(context.Background(), server.Client(), server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty tag_name")
}

func TestIsBehind_truthTable(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"2.8.4", "2.8.5", true},
		{"2.8.5", "2.8.5", false},
		{"v2.8.5", "2.8.5", false}, // v-prefix tolerated on either side
		{"2.8.5", "v2.8.5", false},
		{"devel", "2.8.5", false}, // local dev builds never nudge
		{"", "2.8.5", false},      // safety: empty current
		{"2.8.5", "", false},      // safety: empty latest
		{"2.9.0", "2.8.5", false}, // rolled-back/local-ahead binary must NOT nudge
		{"2.8.4-rc1", "2.8.4", true},
		{"not-a-version", "2.8.5", false}, // invalid semver: nudge suppressed
	}

	for _, tc := range cases {
		t.Run(tc.current+"_vs_"+tc.latest, func(t *testing.T) {
			assert.Equal(t, tc.want, IsBehind(tc.current, tc.latest))
		})
	}
}
