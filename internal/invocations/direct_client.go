package invocations

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/retryhttp"
	"github.com/hashicorp/go-retryablehttp"
)

// XcodeServiceDefaultBaseURL is the prod xcode-analytics-service host.
// For local hackathon use, pass "http://localhost:3000" instead.
const XcodeServiceDefaultBaseURL = "https://xcode-analytics.services.bitrise.io"

// DirectClient talks straight to xcode-analytics-service, bypassing the
// bitrise-website BuildCache UI controller. Useful for local dev/hackathon
// against a `make dev-up` stack — and for any consumer that already holds
// a workspace-scoped PAT.
type DirectClient struct {
	httpClient    *retryablehttp.Client
	baseURL       string
	personalToken string
	workspaceSlug string
	logger        log.Logger
}

// NewDirectClient creates a DirectClient.
//
//   - baseURL — defaults to XcodeServiceDefaultBaseURL when empty
//   - personalAccessToken — Bitrise PAT
//   - workspaceSlug — workspace (org) slug. Sent as the username half of
//     the "Bearer <orgSlug>:<PAT>" auth scheme the service expects.
func NewDirectClient(baseURL, personalAccessToken, workspaceSlug string, logger log.Logger) *DirectClient {
	if baseURL == "" {
		baseURL = XcodeServiceDefaultBaseURL
	}

	httpClient := retryhttp.NewClient(logger)
	httpClient.RetryMax = maxRetries
	httpClient.HTTPClient.Timeout = requestTimeout

	return &DirectClient{
		httpClient:    httpClient,
		baseURL:       baseURL,
		personalToken: personalAccessToken,
		workspaceSlug: workspaceSlug,
		logger:        logger,
	}
}

// XcodeInvocation matches the JSON shape returned by xcode-analytics-service
// `GET /internal/invocations` — `internal/model/types.go:Invocation` in that repo.
type XcodeInvocation struct {
	InvocationID         string            `json:"invocationId"`
	InvocationDate       time.Time         `json:"invocationDate"`
	CreatedAt            time.Time         `json:"createdAt"`
	BitriseOrgSlug       string            `json:"bitriseOrgSlug"`
	BitriseAppSlug       string            `json:"bitriseAppSlug"`
	BitriseBuildSlug     string            `json:"bitriseBuildSlug"`
	BitriseStepID        string            `json:"bitriseStepId"`
	Hostname             string            `json:"hostname"`
	Username             string            `json:"username"`
	CommitHash           string            `json:"commitHash"`
	Branch               string            `json:"branch"`
	RepositoryURL        string            `json:"repositoryUrl"`
	CommitEmail          string            `json:"commitEmail"`
	Command              string            `json:"command"`
	FullCommand          string            `json:"fullCommand"`
	DurationMs           int64             `json:"durationMs"`
	HitRate              float32           `json:"hitRate"`
	Success              bool              `json:"success"`
	Error                string            `json:"error"`
	XcodeVersion         string            `json:"xcodeVersion"`
	WorkflowName         string            `json:"workflowName"`
	ProviderID           string            `json:"providerId"`
	CLIVersion           string            `json:"cliVersion"`
	Envs                 map[string]string `json:"envs"`
	OS                   string            `json:"os"`
	HwCPUCores           int               `json:"hwCpuCores"`
	HwMemSize            int64             `json:"hwMemSize"`
	Datacenter           string            `json:"datacenter"`
	DefaultCharset       string            `json:"defaultCharset"`
	Locale               string            `json:"locale"`
	ToolBuildNumber      string            `json:"toolBuildNumber"`
	ExternalAppID        string            `json:"externalAppId"`
	ExternalWorkflowName string            `json:"externalWorkflowName"`
	ExternalBuildID      string            `json:"externalBuildId"`
	BenchmarkPhase       string            `json:"benchmarkPhase"`
}

// DirectListFilter mirrors the query params accepted by the service's
// `GET /internal/invocations` handler.
type DirectListFilter struct {
	AppSlug       string
	BuildSlug     string
	WorkflowName  string
	Command       string
	ProviderID    string
	RepositoryURL string
	CommitEmail   string // ACI-4908: server-side user filter (added in xcode-analytics-service)
	Success       *bool
	Before        time.Time
	After         time.Time
	OrderBy       string // "started_at" | "hit_rate" | "duration"
	OrderDir      string // "ASC" | "DESC"
	Limit         int
	Offset        int
}

// DirectListResponse holds the decoded list + the X-Total-Count header.
type DirectListResponse struct {
	Items      []XcodeInvocation
	TotalCount uint64
}

// List calls GET /internal/invocations on xcode-analytics-service.
func (c *DirectClient) List(filter DirectListFilter) (*DirectListResponse, error) {
	if c.workspaceSlug == "" {
		return nil, fmt.Errorf("workspace slug required")
	}

	q := url.Values{}
	q.Set("workspaceId", c.workspaceSlug)

	if filter.AppSlug != "" {
		q.Set("appSlug", filter.AppSlug)
	}
	if filter.BuildSlug != "" {
		q.Set("buildSlug", filter.BuildSlug)
	}
	if filter.WorkflowName != "" {
		q.Set("workflowName", filter.WorkflowName)
	}
	if filter.Command != "" {
		q.Set("command", filter.Command)
	}
	if filter.ProviderID != "" {
		q.Set("providerId", filter.ProviderID)
	}
	if filter.RepositoryURL != "" {
		q.Set("repositoryUrl", filter.RepositoryURL)
	}
	if filter.CommitEmail != "" {
		q.Set("commitEmail", filter.CommitEmail)
	}
	if filter.Success != nil {
		q.Set("success", strconv.FormatBool(*filter.Success))
	}
	if !filter.Before.IsZero() {
		q.Set("before", filter.Before.UTC().Format(time.RFC3339))
	}
	if !filter.After.IsZero() {
		q.Set("after", filter.After.UTC().Format(time.RFC3339))
	}
	if filter.OrderBy != "" {
		q.Set("orderBy", filter.OrderBy)
	}
	if filter.OrderDir != "" {
		q.Set("orderDirection", filter.OrderDir)
	}
	if filter.Limit > 0 {
		q.Set("limit", strconv.Itoa(filter.Limit))
	}
	if filter.Offset > 0 {
		q.Set("offset", strconv.Itoa(filter.Offset))
	}

	requestURL := c.baseURL + "/internal/invocations?" + q.Encode()
	c.logger.Debugf("HTTP GET: %s", requestURL)

	req, err := retryablehttp.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create HTTP request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader())
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perform HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)

		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}

	var items []XcodeInvocation
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	totalCount, _ := strconv.ParseUint(resp.Header.Get("X-Total-Count"), 10, 64)

	return &DirectListResponse{
		Items:      items,
		TotalCount: totalCount,
	}, nil
}

// authHeader builds the Authorization value the service expects.
//
// Service auth scheme: "Bearer <orgSlug>:<PAT>" in production. In development
// mode (`ENVIRONMENT=development`) the service treats the entire token as the
// org slug, so callers can pass an empty PAT and we send just "Bearer <orgSlug>".
func (c *DirectClient) authHeader() string {
	if c.personalToken == "" {
		return "Bearer " + c.workspaceSlug
	}

	return "Bearer " + c.workspaceSlug + ":" + c.personalToken
}

// Get calls GET /internal/invocations/{id} on xcode-analytics-service.
func (c *DirectClient) Get(invocationID string) (*XcodeInvocation, error) {
	if invocationID == "" {
		return nil, fmt.Errorf("invocation ID required")
	}

	requestURL := c.baseURL + "/internal/invocations/" + url.PathEscape(invocationID)
	c.logger.Debugf("HTTP GET: %s", requestURL)

	req, err := retryablehttp.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create HTTP request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader())
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perform HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)

		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}

	var inv XcodeInvocation
	if err := json.NewDecoder(resp.Body).Decode(&inv); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &inv, nil
}
