//go:build unit

package common

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

func autoLive(t *testing.T) {
	t.Helper()

	autoActivationLive = true
	t.Cleanup(func() { autoActivationLive = autoActivationEndpointShipped })
}

func TestAutoActivationEnabled_AllowsEverythingWhileTheEndpointDoesNotExist(t *testing.T) {
	require.False(t, autoActivationEndpointShipped, "this test describes the pre-ship state")
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(srv.Close)

	assert.True(t, AutoActivationEnabled(t.Context(), srv.URL, credFor("ws-1"), AppIdentity{}, testLogger()))
	assert.EqualValues(t, 0, calls.Load(), "nothing is asked before the endpoint ships")
}

func TestAutoActivationEnabled_Answers(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"enabled", http.StatusOK, `{"enabled":true}`, true},
		{"disabled", http.StatusOK, `{"enabled":false}`, false},
		{"an error is a no", http.StatusInternalServerError, "", false},
		{"a not found is a no", http.StatusNotFound, "", false},
		{"a forbidden is a no", http.StatusForbidden, "", false},
		{"an undecodable answer is a no", http.StatusOK, "<html>", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			autoLive(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/build-cache/ws-1/auto_activation", r.URL.Path)
				assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)

			assert.Equal(t, tt.want, AutoActivationEnabled(t.Context(), srv.URL, credFor("ws-1"), AppIdentity{}, testLogger()))
		})
	}
}

func TestAutoActivationEnabled_UnreachableIsANo(t *testing.T) {
	autoLive(t)
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()

	assert.False(t, AutoActivationEnabled(t.Context(), srv.URL, credFor("ws-1"), AppIdentity{}, testLogger()))
}

func TestAutoActivationEnabled_SendsTheAppAndWorkflow(t *testing.T) {
	tests := []struct {
		name  string
		app   AppIdentity
		query string
	}{
		{"a Bitrise workflow", AppIdentity{BitriseAppSlug: "app-1", BitriseWorkflowName: "primary"}, "app_slug=app-1&workflow_name=primary"},
		{"an external job", AppIdentity{ExternalAppID: "org/repo", ExternalWorkflowName: "build"}, "external_app_id=org%2Frepo&external_workflow_name=build"},
		{"nothing known", AppIdentity{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			autoLive(t)
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.RawQuery
				_, _ = w.Write([]byte(`{"enabled":true}`))
			}))
			t.Cleanup(srv.Close)

			require.True(t, AutoActivationEnabled(t.Context(), srv.URL, credFor("ws-1"), tt.app, testLogger()))

			assert.Equal(t, tt.query, got)
		})
	}
}

func TestNewAppIdentity(t *testing.T) {
	osProxy := &mocks.OsProxyMock{HostnameFunc: func() (string, error) { return "build-vm", nil }}

	assert.Equal(t, AppIdentity{BitriseAppSlug: "app-1", BitriseWorkflowName: "primary"},
		NewAppIdentity(map[string]string{"BITRISE_IO": "true", "BITRISE_BUILD_SLUG": "b", "BITRISE_APP_SLUG": "app-1", "BITRISE_TRIGGERED_WORKFLOW_ID": "primary"}, osProxy))
	assert.Equal(t, AppIdentity{ExternalAppID: "org/repo", ExternalWorkflowName: "build"},
		NewAppIdentity(map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_REPOSITORY": "org/repo", "GITHUB_JOB": "build", "BITRISE_APP_SLUG": "ignored"}, osProxy))
	assert.Equal(t, AppIdentity{}, NewAppIdentity(map[string]string{}, osProxy))
}
