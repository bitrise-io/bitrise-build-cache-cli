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

const (
	// DefaultBaseURL is the bitrise-website host that serves the build-cache UI APIs.
	DefaultBaseURL = "https://app.bitrise.io"

	maxRetries     = 3
	requestTimeout = 30 * time.Second
)

// Client fetches invocation data from the bitrise-website BuildCache::InvocationsController.
type Client struct {
	httpClient    *retryablehttp.Client
	baseURL       string
	personalToken string
	workspaceSlug string
	logger        log.Logger
}

// NewClient creates an invocations Client.
//
//   - baseURL — defaults to DefaultBaseURL when empty
//   - personalAccessToken — Bitrise PAT (NOT the build-cache auth token); sent as
//     "Authorization: token <pat>" matching authenticate_user_from_personal_access_token!
//   - workspaceSlug — the workspace (account) slug used in the URL path
func NewClient(baseURL, personalAccessToken, workspaceSlug string, logger log.Logger) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	httpClient := retryhttp.NewClient(logger)
	httpClient.RetryMax = maxRetries
	httpClient.HTTPClient.Timeout = requestTimeout

	return &Client{
		httpClient:    httpClient,
		baseURL:       baseURL,
		personalToken: personalAccessToken,
		workspaceSlug: workspaceSlug,
		logger:        logger,
	}
}

// List returns recent invocations matching filter.
// GET /build-cache/:workspace_slug/invocations.json
func (c *Client) List(filter ListFilter) (*ListResponse, error) {
	if c.workspaceSlug == "" {
		return nil, fmt.Errorf("workspace slug required")
	}

	q := url.Values{}
	if filter.Tool != "" {
		q.Set("tool", filter.Tool)
	}
	if filter.Page > 0 {
		q.Set("page", strconv.Itoa(filter.Page))
	}
	if filter.ItemsPerPage > 0 {
		q.Set("items_per_page", strconv.Itoa(filter.ItemsPerPage))
	}
	if filter.OrderBy != "" {
		q.Set("order_by", filter.OrderBy)
	}
	if filter.OrderDirection != "" {
		q.Set("order_direction", filter.OrderDirection)
	}
	if filter.ProjectSlug != "" {
		q.Set("project_slug", filter.ProjectSlug)
	}
	if filter.BuildSlug != "" {
		q.Set("build_slug", filter.BuildSlug)
	}
	if filter.RepositoryURL != "" {
		q.Set("repository_url", filter.RepositoryURL)
	}
	if filter.Workflow != "" {
		q.Set("workflow", filter.Workflow)
	}
	if filter.CIProvider != "" {
		q.Set("ci_provider", filter.CIProvider)
	}
	if filter.Status != "" {
		q.Set("status", filter.Status)
	}
	if filter.Command != "" {
		q.Set("command", filter.Command)
	}
	// before/after on the API are millisecond epoch ints.
	if !filter.Before.IsZero() {
		q.Set("before", strconv.FormatInt(filter.Before.UnixMilli(), 10))
	}
	if !filter.After.IsZero() {
		q.Set("after", strconv.FormatInt(filter.After.UnixMilli(), 10))
	}

	path := fmt.Sprintf("/build-cache/%s/invocations.json", url.PathEscape(c.workspaceSlug))
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var resp ListResponse
	if err := c.getJSON(path, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// Get returns the full invocation detail.
// GET /build-cache/:workspace_slug/invocations/:build_tool/:invocation_id.json
func (c *Client) Get(buildTool, invocationID string) (json.RawMessage, error) {
	if err := requireBuildTool(buildTool); err != nil {
		return nil, err
	}
	if invocationID == "" {
		return nil, fmt.Errorf("invocation ID required")
	}

	path := fmt.Sprintf("/build-cache/%s/invocations/%s/%s.json",
		url.PathEscape(c.workspaceSlug),
		url.PathEscape(buildTool),
		url.PathEscape(invocationID),
	)

	var raw json.RawMessage
	if err := c.getJSON(path, &raw); err != nil {
		return nil, err
	}

	return raw, nil
}

// GetSummary is Get + decode into InvocationSummary.
func (c *Client) GetSummary(buildTool, invocationID string) (*InvocationSummary, error) {
	raw, err := c.Get(buildTool, invocationID)
	if err != nil {
		return nil, err
	}

	var s InvocationSummary
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("decode invocation summary: %w", err)
	}

	return &s, nil
}

// GetGradleTasks fetches task-level data for a Gradle invocation.
// GET /build-cache/:workspace_slug/invocations/gradle/:invocation_id/tasks.json
func (c *Client) GetGradleTasks(invocationID string) (*GradleTasksResponse, error) {
	if invocationID == "" {
		return nil, fmt.Errorf("invocation ID required")
	}

	path := fmt.Sprintf("/build-cache/%s/invocations/gradle/%s/tasks.json",
		url.PathEscape(c.workspaceSlug),
		url.PathEscape(invocationID),
	)

	var resp GradleTasksResponse
	if err := c.getJSON(path, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// GetBazelTargets fetches target-level data for a Bazel invocation.
// GET /build-cache/:workspace_slug/invocations/bazel/:invocation_id/targets.json
func (c *Client) GetBazelTargets(invocationID string) (*BazelTargetsResponse, error) {
	if invocationID == "" {
		return nil, fmt.Errorf("invocation ID required")
	}

	path := fmt.Sprintf("/build-cache/%s/invocations/bazel/%s/targets.json",
		url.PathEscape(c.workspaceSlug),
		url.PathEscape(invocationID),
	)

	var resp BazelTargetsResponse
	if err := c.getJSON(path, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// GetChildInvocations fetches child invocations for a React Native parent.
// GET /build-cache/:workspace_slug/invocations/react-native/:invocation_id/child-invocations.json
func (c *Client) GetChildInvocations(invocationID string) ([]ChildInvocationGroup, error) {
	if invocationID == "" {
		return nil, fmt.Errorf("invocation ID required")
	}

	path := fmt.Sprintf("/build-cache/%s/invocations/react-native/%s/child-invocations.json",
		url.PathEscape(c.workspaceSlug),
		url.PathEscape(invocationID),
	)

	var groups []ChildInvocationGroup
	if err := c.getJSON(path, &groups); err != nil {
		return nil, err
	}

	return groups, nil
}

// GetSiblingInvocations fetches sibling invocations under the same parent.
// GET /build-cache/:workspace_slug/invocations/:build_tool/:invocation_id/sibling-invocations.json
func (c *Client) GetSiblingInvocations(buildTool, invocationID string) ([]ChildInvocationGroup, error) {
	if err := requireBuildTool(buildTool); err != nil {
		return nil, err
	}
	if invocationID == "" {
		return nil, fmt.Errorf("invocation ID required")
	}

	path := fmt.Sprintf("/build-cache/%s/invocations/%s/%s/sibling-invocations.json",
		url.PathEscape(c.workspaceSlug),
		url.PathEscape(buildTool),
		url.PathEscape(invocationID),
	)

	var groups []ChildInvocationGroup
	if err := c.getJSON(path, &groups); err != nil {
		return nil, err
	}

	return groups, nil
}

// Private — HTTP plumbing

func requireBuildTool(buildTool string) error {
	switch buildTool {
	case BuildToolGradle, BuildToolBazel, BuildToolXcode, BuildToolReactNative, BuildToolCcache:
		return nil
	default:
		return fmt.Errorf("invalid build tool %q", buildTool)
	}
}

func (c *Client) getJSON(path string, out any) error {
	requestURL := c.baseURL + path
	c.logger.Debugf("HTTP GET: %s", requestURL)

	req, err := retryablehttp.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("create HTTP request: %w", err)
	}
	if c.personalToken != "" {
		// Bitrise PAT scheme: "Authorization: token <pat>".
		req.Header.Set("Authorization", "token "+c.personalToken)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("perform HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)

		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}
