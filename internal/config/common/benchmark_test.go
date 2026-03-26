//go:build unit

package common

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestWriteBenchmarkPhaseFile(t *testing.T) {
	logger := log.NewLogger()

	t.Run("creates file with correct content", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		WriteBenchmarkPhaseFile("baseline", logger)

		filePath := filepath.Join(home, ".local", "state", "xcelerate", "benchmark", "benchmark-phase.json")
		assert.FileExists(t, filePath)

		data, err := os.ReadFile(filePath)
		require.NoError(t, err)

		var result BenchmarkPhaseFile
		require.NoError(t, json.Unmarshal(data, &result))
		assert.Equal(t, "baseline", result.Phase)
	})

	t.Run("overwrites existing file", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		WriteBenchmarkPhaseFile("baseline", logger)
		WriteBenchmarkPhaseFile("warmup", logger)

		filePath := filepath.Join(home, ".local", "state", "xcelerate", "benchmark", "benchmark-phase.json")
		data, err := os.ReadFile(filePath)
		require.NoError(t, err)

		var result BenchmarkPhaseFile
		require.NoError(t, json.Unmarshal(data, &result))
		assert.Equal(t, "warmup", result.Phase)
	})
}

func TestReadBenchmarkPhaseFile(t *testing.T) {
	logger := log.NewLogger()

	t.Run("reads written phase", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		WriteBenchmarkPhaseFile("established", logger)

		phase := ReadBenchmarkPhaseFile(logger)
		assert.Equal(t, "established", phase)
	})

	t.Run("returns empty when file does not exist", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		phase := ReadBenchmarkPhaseFile(logger)
		assert.Empty(t, phase)
	})

	t.Run("returns empty for malformed JSON", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		dir := filepath.Join(home, ".local", "state", "xcelerate", "benchmark")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "benchmark-phase.json"), []byte("not json"), 0o644)) //nolint:mnd,gosec

		phase := ReadBenchmarkPhaseFile(logger)
		assert.Empty(t, phase)
	})
}
