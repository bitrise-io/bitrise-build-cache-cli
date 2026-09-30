// Package githuboidc exchanges a GitHub Actions OIDC token for a short-lived
// Bitrise Workspace API token under a Bitrise OIDC trust policy, and exchanges
// again before that token expires. L3, beside oauth and buildhub; see docs/auth.md.
package githuboidc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

// audience is the only one the Bitrise exchange accepts.
const audience = "app.bitrise.io"

// Trust policies allow 15 minutes at the shortest, so this still leaves most of the lifetime.
const refreshSkew = 5 * time.Minute

const requestTimeout = 10 * time.Second

// A policy that rejects one GitHub token rejects the next, and every attempt spends one.
const failureBackoff = 30 * time.Second

const maxErrorBody = 512

var (
	errEmptyIDToken     = errors.New("GitHub returned an empty OIDC token")
	errEmptyAccessToken = errors.New("the Bitrise OIDC exchange returned an empty token")
	errNoExpiry         = errors.New("the Bitrise OIDC exchange returned no expires_in")
)

// RejectedError is the exchange refusing the token. The endpoint gives the same
// `invalid_token` for every cause, so the claims it carries are what the user
// needs to see to write a matching policy.
type RejectedError struct {
	PolicyID string
	Status   int
	Body     string
	Claims   Claims
}

// Claims are the ones a trust policy most often matches on. Read without
// verifying the signature: they are for the error message, never for a decision.
type Claims struct {
	Sub        string `json:"sub"`
	Repository string `json:"repository"`
	Ref        string `json:"ref"`
	EventName  string `json:"event_name"`
}

func (e *RejectedError) Error() string {
	msg := fmt.Sprintf("Bitrise rejected this job's GitHub Actions OIDC token for trust policy %s (HTTP %d: %s)",
		e.PolicyID, e.Status, e.Body)
	if e.Claims == (Claims{}) {
		return msg
	}

	return msg + fmt.Sprintf(". The policy's matching rules must equal the token's claims exactly; this job's are "+
		"sub=%q repository=%q ref=%q event_name=%q. To cover more than one branch or event with one policy, "+
		"customize the sub claim in GitHub", e.Claims.Sub, e.Claims.Repository, e.Claims.Ref, e.Claims.EventName)
}

// Client exchanges GitHub OIDC tokens for Bitrise tokens and caches the result
// until it approaches expiry. It is safe for concurrent use.
type Client struct {
	requestURL   string
	requestToken string
	policyID     string
	endpoint     string
	httpClient   *http.Client
	now          func() time.Time

	// Serialises exchanges; a channel rather than a mutex so waiting honours the caller's context.
	refreshSlot chan struct{}
	mu          sync.RWMutex
	token       string
	expiresAt   time.Time
	lastErr     error
	lastErrAt   time.Time
}

// settings is the normalized configuration, so the Shared cache key and the client
// it caches can't disagree about what the environment means.
type settings struct {
	requestURL   string
	requestToken string
	policyID     string
	endpoint     string
}

func settingsFrom(envs map[string]string) settings {
	endpoint := strings.TrimSpace(envs[auth.EnvOIDCTokenEndpoint])
	if endpoint == "" {
		endpoint = auth.DefaultOIDCTokenEndpoint
	}

	return settings{
		requestURL:   envs[auth.EnvGitHubOIDCRequestURL],
		requestToken: envs[auth.EnvGitHubOIDCRequestToken],
		policyID:     strings.TrimSpace(envs[auth.EnvOIDCPolicyID]),
		endpoint:     endpoint,
	}
}

// FromEnv builds a Client when a trust policy is configured and GitHub offers
// the job an OIDC token.
func FromEnv(envs map[string]string) (*Client, bool) {
	if !auth.OnGitHubActionsOIDC(envs) {
		return nil, false
	}
	s := settingsFrom(envs)

	return &Client{
		requestURL:   s.requestURL,
		requestToken: s.requestToken,
		policyID:     s.policyID,
		endpoint:     s.endpoint,
		httpClient:   &http.Client{Timeout: requestTimeout},
		now:          time.Now,
		refreshSlot:  make(chan struct{}, 1),
	}, true
}

// Process-wide, so every resolver in one command shares a single cached exchange.
var shared sync.Map //nolint:gochecknoglobals

func Shared(envs map[string]string) (*Client, bool) {
	s := settingsFrom(envs)
	key := strings.Join([]string{s.requestURL, s.requestToken, s.policyID, s.endpoint}, "\x00")
	if c, ok := shared.Load(key); ok {
		return c.(*Client), true //nolint:forcetypeassert // only *Client is stored
	}

	c, ok := FromEnv(envs)
	if !ok {
		return nil, false
	}

	actual, _ := shared.LoadOrStore(key, c)

	return actual.(*Client), true //nolint:forcetypeassert // only *Client is stored
}

// Token returns a Bitrise token, exchanging a fresh GitHub token when the cached
// one is missing or close to expiry.
func (c *Client) Token(ctx context.Context) (string, time.Time, error) {
	if token, expiresAt, ok := c.cached(); ok {
		return token, expiresAt, nil
	}

	select {
	case c.refreshSlot <- struct{}{}:
		defer func() { <-c.refreshSlot }()
	case <-ctx.Done():
		// Rather than queue behind another exchange, spend what is left of the
		// caller's deadline on the token we already hold.
		if token, expiresAt, ok := c.unexpired(); ok {
			return token, expiresAt, nil
		}

		return "", time.Time{}, fmt.Errorf("waiting for the GitHub Actions OIDC exchange: %w", ctx.Err())
	}

	// Another goroutine may have exchanged while this one waited for the slot.
	if token, expiresAt, ok := c.cached(); ok {
		return token, expiresAt, nil
	}

	err := c.recentFailure()
	if err == nil {
		var token string
		var expiresAt time.Time
		if token, expiresAt, err = c.refresh(ctx); err == nil {
			return token, expiresAt, nil
		}
		c.recordFailure(err)
	}

	// A failed early refresh must not cost the caller a token that still works.
	if token, expiresAt, ok := c.unexpired(); ok {
		return token, expiresAt, nil
	}

	return "", time.Time{}, err
}

func (c *Client) refresh(ctx context.Context) (string, time.Time, error) {
	// A new GitHub token every time: the exchange accepts each one only once.
	idToken, err := c.fetchIDToken(ctx)
	if err != nil {
		return "", time.Time{}, err
	}

	return c.exchange(ctx, idToken)
}

func (c *Client) fetchIDToken(ctx context.Context) (string, error) {
	u, err := url.Parse(c.requestURL)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", auth.EnvGitHubOIDCRequestURL, err)
	}
	q := u.Query()
	q.Set("audience", audience)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("create GitHub OIDC token request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.requestToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request a GitHub OIDC token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("request a GitHub OIDC token: HTTP %d: %s", resp.StatusCode, readErrorBody(resp.Body)) //nolint:err113
	}

	var result struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode GitHub OIDC token response: %w", err)
	}
	if result.Value == "" {
		return "", errEmptyIDToken
	}

	return result.Value, nil
}

func (c *Client) exchange(ctx context.Context, idToken string) (string, time.Time, error) {
	form := url.Values{
		"grant_type":         {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token_type": {"id_token"},
		"policy_id":          {c.policyID},
		"subject_token":      {idToken},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create OIDC exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("exchange the GitHub OIDC token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest && resp.StatusCode < http.StatusInternalServerError {
		return "", time.Time{}, &RejectedError{
			PolicyID: c.policyID,
			Status:   resp.StatusCode,
			Body:     readErrorBody(resp.Body),
			Claims:   claimsOf(idToken),
		}
	}
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("exchange the GitHub OIDC token: HTTP %d: %s", resp.StatusCode, readErrorBody(resp.Body)) //nolint:err113
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", time.Time{}, fmt.Errorf("decode OIDC exchange response: %w", err)
	}
	if result.AccessToken == "" {
		return "", time.Time{}, errEmptyAccessToken
	}
	if result.ExpiresIn <= 0 {
		return "", time.Time{}, errNoExpiry
	}

	expiresAt := c.now().Add(time.Duration(result.ExpiresIn) * time.Second)
	c.store(result.AccessToken, expiresAt)

	return result.AccessToken, expiresAt, nil
}

func readErrorBody(r io.Reader) string {
	body, _ := io.ReadAll(io.LimitReader(r, maxErrorBody))

	return strings.TrimSpace(string(body))
}

func claimsOf(token string) Claims {
	parts := strings.Split(token, ".")
	if len(parts) != 3 { //nolint:mnd
		return Claims{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return Claims{}
	}

	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}
	}

	return claims
}

func (c *Client) cached() (string, time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.token != "" && c.now().Add(refreshSkew).Before(c.expiresAt) {
		return c.token, c.expiresAt, true
	}

	return "", time.Time{}, false
}

// unexpired is cached without the safety skew: close enough to expiry that we would
// rather exchange, but still better than failing the caller outright.
func (c *Client) unexpired() (string, time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.token != "" && c.now().Before(c.expiresAt) {
		return c.token, c.expiresAt, true
	}

	return "", time.Time{}, false
}

func (c *Client) recentFailure() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.lastErr != nil && c.now().Sub(c.lastErrAt) < failureBackoff {
		return c.lastErr
	}

	return nil
}

func (c *Client) recordFailure(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastErr, c.lastErrAt = err, c.now()
}

func (c *Client) store(token string, expiresAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token, c.expiresAt, c.lastErr = token, expiresAt, nil
}
