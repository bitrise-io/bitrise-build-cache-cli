//go:build unit

package common

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBenchmarkPhase_BitriseProvider(t *testing.T) {
	t.Parallel()

	var capturedURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(benchmarkResponse{Phase: "baseline"}) //nolint:errcheck
	}))
	defer server.Close()

	client := NewBenchmarkPhaseClient(server.URL, CacheAuthConfig{
		AuthToken:   "test-token",
		WorkspaceID: "ws-123",
	}, log.NewLogger())

	phase, err := client.GetBenchmarkPhase(BuildToolGradle, CacheConfigMetadata{
		CIProvider:          CIProviderBitrise,
		BitriseAppID:        "app-slug-1",
		BitriseWorkflowName: "primary",
	})

	require.NoError(t, err)
	assert.Equal(t, "baseline", phase)
	assert.Contains(t, capturedURL, "/build-cache/ws-123/invocations/gradle/command_benchmark_status")
	assert.Contains(t, capturedURL, "app_slug=app-slug-1")
	assert.Contains(t, capturedURL, "workflow_name=primary")
}

func TestGetBenchmarkPhase_ExternalProvider(t *testing.T) {
	t.Parallel()

	var capturedURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(benchmarkResponse{Phase: "warmup"}) //nolint:errcheck
	}))
	defer server.Close()

	client := NewBenchmarkPhaseClient(server.URL, CacheAuthConfig{
		AuthToken:   "test-token",
		WorkspaceID: "ws-456",
	}, log.NewLogger())

	phase, err := client.GetBenchmarkPhase(BuildToolGradle, CacheConfigMetadata{
		CIProvider:           CIProviderGitHubActions,
		ExternalAppID:        "org/my-repo",
		ExternalWorkflowName: "build",
	})

	require.NoError(t, err)
	assert.Equal(t, "warmup", phase)
	assert.Contains(t, capturedURL, "external_app_id=org%2Fmy-repo")
	assert.Contains(t, capturedURL, "external_workflow_name=build")
}

func TestGetBenchmarkPhase_EmptyIdentifiers(t *testing.T) {
	t.Parallel()

	client := NewBenchmarkPhaseClient("http://unused", CacheAuthConfig{
		AuthToken:   "test-token",
		WorkspaceID: "ws-123",
	}, log.NewLogger())

	// Bitrise with empty app ID
	phase, err := client.GetBenchmarkPhase(BuildToolGradle, CacheConfigMetadata{
		CIProvider: CIProviderBitrise,
	})
	require.NoError(t, err)
	assert.Empty(t, phase)

	// Non-Bitrise with empty external IDs
	phase, err = client.GetBenchmarkPhase(BuildToolGradle, CacheConfigMetadata{
		CIProvider: CIProviderCircleCI,
	})
	require.NoError(t, err)
	assert.Empty(t, phase)
}

func TestGetBenchmarkPhase_EmptyWorkspaceID(t *testing.T) {
	t.Parallel()

	client := NewBenchmarkPhaseClient("http://unused", CacheAuthConfig{
		AuthToken: "test-token",
	}, log.NewLogger())

	phase, err := client.GetBenchmarkPhase(BuildToolGradle, CacheConfigMetadata{
		CIProvider:   CIProviderBitrise,
		BitriseAppID: "app-1",
	})
	require.NoError(t, err)
	assert.Empty(t, phase)
}

func TestGetBenchmarkPhase_HTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error")) //nolint:errcheck
	}))
	defer server.Close()

	client := NewBenchmarkPhaseClient(server.URL, CacheAuthConfig{
		AuthToken:   "test-token",
		WorkspaceID: "ws-123",
	}, log.NewLogger())
	client.httpClient.RetryMax = 0

	phase, err := client.GetBenchmarkPhase(BuildToolGradle, CacheConfigMetadata{
		CIProvider:          CIProviderBitrise,
		BitriseAppID:        "app-1",
		BitriseWorkflowName: "primary",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
	assert.Empty(t, phase)
}

func TestGetBenchmarkPhase_MalformedJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json")) //nolint:errcheck
	}))
	defer server.Close()

	client := NewBenchmarkPhaseClient(server.URL, CacheAuthConfig{
		AuthToken:   "test-token",
		WorkspaceID: "ws-123",
	}, log.NewLogger())

	phase, err := client.GetBenchmarkPhase(BuildToolGradle, CacheConfigMetadata{
		CIProvider:          CIProviderBitrise,
		BitriseAppID:        "app-1",
		BitriseWorkflowName: "primary",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode response")
	assert.Empty(t, phase)
}

func TestGetBenchmarkPhase_EmptyPhase(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(benchmarkResponse{Phase: ""}) //nolint:errcheck
	}))
	defer server.Close()

	client := NewBenchmarkPhaseClient(server.URL, CacheAuthConfig{
		AuthToken:   "test-token",
		WorkspaceID: "ws-123",
	}, log.NewLogger())

	phase, err := client.GetBenchmarkPhase(BuildToolGradle, CacheConfigMetadata{
		CIProvider:          CIProviderBitrise,
		BitriseAppID:        "app-1",
		BitriseWorkflowName: "primary",
	})

	require.NoError(t, err)
	assert.Empty(t, phase)
}
