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

// ErrNotFound is returned by Get when the requested invocation does not exist.
var ErrNotFound = fmt.Errorf("invocation not found")

// ProviderIDLocal is the magic value for the `providerId` query param that
// matches invocations recorded with an empty CI provider — i.e. local builds.
// The service maps `providerId=unknown` to `WHERE provider_id = ”` (see
// xcode-analytics-service `parseQueryParams`). Verified for ACI-4910:
// existing CI vs local split is sufficient, no schema change needed.
const ProviderIDLocal = "unknown"

// DirectClient talks straight to one of the analytics sink services
// (xcode / gradle / multiplatform), bypassing the bitrise-website BuildCache
// UI controller. Useful for local dev/hackathon against a `make dev-up`
// stack and for any consumer that already holds a workspace-scoped PAT.
//
// The per-tool path differences (xcode/multiplatform use
// `/internal/invocations`, gradle uses `/builds`) are captured by the
// `ServiceProfile` selected at construction.
type DirectClient struct {
	httpClient    *retryablehttp.Client
	baseURL       string
	personalToken string
	workspaceSlug string
	profile       ServiceProfile
	logger        log.Logger
}

// NewDirectClient creates a DirectClient targeting xcode-analytics-service.
// Kept as a backward-compatible shorthand; new code should prefer
// `NewDirectClientForTool` or `NewDirectClientWithProfile`.
//
//   - baseURL — defaults to XcodeProfile.DefaultBaseURL when empty
//   - personalAccessToken — Bitrise PAT
//   - workspaceSlug — workspace (org) slug. Sent as the username half of
//     the "Bearer <orgSlug>:<PAT>" auth scheme the service expects.
func NewDirectClient(baseURL, personalAccessToken, workspaceSlug string, logger log.Logger) *DirectClient {
	return NewDirectClientWithProfile(XcodeProfile, baseURL, personalAccessToken, workspaceSlug, logger)
}

// NewDirectClientForTool resolves the right `ServiceProfile` for the given
// build-tool key and returns a DirectClient pointed at it. Returns an error
// for tools without a profile (e.g. bazel — different filter shape).
func NewDirectClientForTool(tool, baseURL, personalAccessToken, workspaceSlug string, logger log.Logger) (*DirectClient, error) {
	profile, ok := ProfileForTool(tool)
	if !ok {
		return nil, fmt.Errorf("no DirectClient profile for tool %q", tool)
	}

	return NewDirectClientWithProfile(profile, baseURL, personalAccessToken, workspaceSlug, logger), nil
}

// NewDirectClientWithProfile is the explicit form: pass the profile yourself.
//
//   - baseURL — overrides `profile.DefaultBaseURL` when non-empty
//     (use `profile.LocalBaseURL` for the local `make dev-up` stack).
func NewDirectClientWithProfile(profile ServiceProfile, baseURL, personalAccessToken, workspaceSlug string, logger log.Logger) *DirectClient {
	if baseURL == "" {
		baseURL = profile.DefaultBaseURL
	}

	httpClient := retryhttp.NewClient(logger)
	httpClient.RetryMax = maxRetries
	httpClient.HTTPClient.Timeout = requestTimeout

	return &DirectClient{
		httpClient:    httpClient,
		baseURL:       baseURL,
		personalToken: personalAccessToken,
		workspaceSlug: workspaceSlug,
		profile:       profile,
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
	Hostname      string // ACI-4909: this-Mac-only filter (added in xcode-analytics-service)
	LocalOnly     bool   // ACI-4910: shorthand for ProviderID = ProviderIDLocal — only matters when ProviderID is empty
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
	switch {
	case filter.ProviderID != "":
		q.Set("providerId", filter.ProviderID)
	case filter.LocalOnly:
		q.Set("providerId", ProviderIDLocal)
	}
	if filter.RepositoryURL != "" {
		q.Set("repositoryUrl", filter.RepositoryURL)
	}
	if filter.CommitEmail != "" {
		q.Set("commitEmail", filter.CommitEmail)
	}
	if filter.Hostname != "" {
		q.Set("hostname", filter.Hostname)
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

	requestURL := c.baseURL + c.profile.InvocationsPath + "?" + q.Encode()
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

// InvocationStats matches the JSON returned by GET /internal/invocations/stats
// on xcode-analytics-service.
//
// `TimeSavedMs` is an estimate computed server-side as
// SUM(duration_ms × hit_rate); see the service's `GetInvocationStats`.
type InvocationStats struct {
	Count       uint64  `json:"count"`
	HitRateP50  float64 `json:"hitRateP50"`
	TimeSavedMs int64   `json:"timeSavedMs"`
}

// Stats returns aggregate metrics over the same filter as List.
// ACI-4911.
func (c *DirectClient) Stats(filter DirectListFilter) (*InvocationStats, error) {
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
	switch {
	case filter.ProviderID != "":
		q.Set("providerId", filter.ProviderID)
	case filter.LocalOnly:
		q.Set("providerId", ProviderIDLocal)
	}
	if filter.RepositoryURL != "" {
		q.Set("repositoryUrl", filter.RepositoryURL)
	}
	if filter.CommitEmail != "" {
		q.Set("commitEmail", filter.CommitEmail)
	}
	if filter.Hostname != "" {
		q.Set("hostname", filter.Hostname)
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

	requestURL := c.baseURL + c.profile.StatsPath + "?" + q.Encode()
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

	var stats InvocationStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &stats, nil
}

// Get calls GET /internal/invocations/{id} on xcode-analytics-service.
func (c *DirectClient) Get(invocationID string) (*XcodeInvocation, error) {
	if invocationID == "" {
		return nil, fmt.Errorf("invocation ID required")
	}

	requestURL := c.baseURL + c.profile.InvocationsPath + "/" + url.PathEscape(invocationID)
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
		return nil, ErrNotFound
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
