// Package bitriseapi is a minimal client for the Bitrise REST API
// (api.bitrise.io). It exists so the OAuth login flow can list the workspaces a
// freshly-minted PAT can access, for the interactive workspace picker.
package bitriseapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultAPIBaseURL is the production Bitrise REST API base URL. Overridable
// via BITRISE_API_BASE_URL (e.g. to target staging).
const DefaultAPIBaseURL = "https://api.bitrise.io/v0.1"

const requestTimeout = 30 * time.Second

// Workspace is a Bitrise organization (workspace) the authenticated user can
// access. Slug is what the build cache expects as the workspace/org id.
type Workspace struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// ResolveAPIBaseURL returns the REST API base URL, honoring BITRISE_API_BASE_URL.
func ResolveAPIBaseURL(envs map[string]string) string {
	if v := envs["BITRISE_API_BASE_URL"]; v != "" {
		return v
	}

	return DefaultAPIBaseURL
}

// ListWorkspaces returns the workspaces (organizations) the PAT can access.
// Endpoint: GET <baseURL>/organizations with "Authorization: token <pat>".
func ListWorkspaces(ctx context.Context, baseURL, pat string) ([]Workspace, error) {
	if baseURL == "" {
		baseURL = DefaultAPIBaseURL
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/organizations"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "token "+pat)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list workspaces: %s returned %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Bitrise REST responses use a {"data": ...} envelope.
	var env struct {
		Data []Workspace `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return env.Data, nil
}
