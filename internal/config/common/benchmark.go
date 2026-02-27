package common

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/retryhttp"
	"github.com/hashicorp/go-retryablehttp"
)

const (
	benchmarkMaxRetries = 3

	BenchmarkPhaseBaseline = "baseline"
	BenchmarkPhaseWarmup   = "warmup"

	BuildToolGradle = "gradle"
	BuildToolXcode  = "xcode"
	BuildToolBazel  = "bazel"
)

type benchmarkResponse struct {
	Phase string `json:"phase"`
}

//go:generate moq -rm -stub -pkg mocks -out ./mocks/benchmark_phase_provider.go . BenchmarkPhaseProvider

// BenchmarkPhaseProvider fetches the benchmark phase for a build.
type BenchmarkPhaseProvider interface {
	GetBenchmarkPhase(buildTool string, metadata CacheConfigMetadata) (string, error)
}

// BenchmarkPhaseClient fetches the benchmark phase for a Gradle build from the Bitrise API.
type BenchmarkPhaseClient struct {
	httpClient *retryablehttp.Client
	baseURL    string
	authConfig CacheAuthConfig
	logger     log.Logger
}

// NewBenchmarkPhaseClient creates a new BenchmarkPhaseClient.
func NewBenchmarkPhaseClient(baseURL string, authConfig CacheAuthConfig, logger log.Logger) *BenchmarkPhaseClient {
	httpClient := retryhttp.NewClient(logger)
	httpClient.RetryMax = benchmarkMaxRetries

	return &BenchmarkPhaseClient{
		httpClient: httpClient,
		baseURL:    baseURL,
		authConfig: authConfig,
		logger:     logger,
	}
}

// GetBenchmarkPhase fetches the benchmark phase for the current build.
// The buildTool parameter specifies the build tool (gradle, xcode, bazel).
// Returns empty string if no benchmark phase is active or if the build can't be identified.
func (c *BenchmarkPhaseClient) GetBenchmarkPhase(buildTool string, metadata CacheConfigMetadata) (string, error) {
	params := url.Values{}

	if c.authConfig.WorkspaceID == "" {
		return "", fmt.Errorf("workspace ID is required to fetch benchmark phase")
	}

	if metadata.CIProvider == CIProviderBitrise {
		if metadata.BitriseAppID == "" || metadata.BitriseWorkflowName == "" {
			c.logger.Debugf("no Bitrise metadata found, skipping benchmark phase check")

			return "", nil
		}
		params.Set("app_slug", metadata.BitriseAppID)
		params.Set("workflow_name", metadata.BitriseWorkflowName)
	} else {
		if metadata.ExternalAppID == "" || metadata.ExternalWorkflowName == "" {
			c.logger.Debugf("no external IDs found, skipping benchmark phase check")

			return "", nil
		}
		params.Set("external_app_id", metadata.ExternalAppID)
		params.Set("external_workflow_name", metadata.ExternalWorkflowName)
	}

	requestURL := fmt.Sprintf("%s/build-cache/%s/invocations/%s/command_benchmark_status?%s",
		c.baseURL, c.authConfig.WorkspaceID, buildTool, params.Encode())

	req, err := retryablehttp.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.authConfig.AuthToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to perform HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)

		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}

	var result benchmarkResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Phase, nil
}
