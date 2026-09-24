// Package buildhub exchanges the per-VM token a Bitrise Build Hub runner carries
// for a short-lived, workspace-scoped Build Cache token. It sits at L3 beside
// oauth: both turn something long-lived into a credential, and neither knows about
// precedence. See docs/auth.md.
package buildhub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

// tokenType selects the Build Cache token. It must match the VMTokenType enum
// name in the instance-manager API.
const tokenType = "VM_TOKEN_TYPE_BUILD_CACHE" //nolint:gosec // enum name, not a credential

// refreshSkew is how long before expiry a token counts as stale, so a build step
// never starts with one that expires underneath it.
const refreshSkew = 5 * time.Minute

// requestTimeout bounds the exchange. This sits in front of every resolve on a
// Build Hub runner, so a hanging instance-manager must not hang the build.
const requestTimeout = 10 * time.Second

var errEmptyToken = errors.New("instance-manager returned an empty token")

// Client exchanges the VM token for a Build Cache token and caches the result
// until it approaches expiry. It is safe for concurrent use.
type Client struct {
	tokenURL   string
	vmToken    string
	httpClient *http.Client

	// refreshSlot serialises exchanges so a burst of per-RPC resolves triggers one
	// request rather than one each. A channel rather than a mutex so waiting honours
	// the caller's context.
	refreshSlot chan struct{}
	mu          sync.RWMutex
	token       string
	expiresAt   time.Time
}

// FromEnv builds a Client when the environment carries both halves of the pair.
// Both or neither: a half-set pair means the runner is not offering to broker, and
// guessing at the other half would produce a confusing failure later.
func FromEnv(envs map[string]string) (*Client, bool) {
	if !auth.OnBuildHub(envs) {
		return nil, false
	}
	tokenURL, vmToken := envs[auth.EnvBuildHubVMTokenURL], envs[auth.EnvBuildHubVMToken]

	return &Client{
		tokenURL:    tokenURL,
		vmToken:     vmToken,
		httpClient:  &http.Client{Timeout: requestTimeout},
		refreshSlot: make(chan struct{}, 1),
		mu:          sync.RWMutex{},
		token:       "",
		expiresAt:   time.Time{},
	}, true
}

// Process-wide, so every resolver in one command shares a single cached exchange.
var shared sync.Map //nolint:gochecknoglobals

func Shared(envs map[string]string) (*Client, bool) {
	key := envs[auth.EnvBuildHubVMTokenURL] + "\x00" + envs[auth.EnvBuildHubVMToken]
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

// Token returns a Build Cache token, exchanging the VM token for a fresh one when
// the cached copy is missing or close to expiry.
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

		return "", time.Time{}, fmt.Errorf("waiting to broker a Build Cache token: %w", ctx.Err())
	}

	// Another goroutine may have exchanged while this one waited for the slot.
	if token, expiresAt, ok := c.cached(); ok {
		return token, expiresAt, nil
	}

	token, expiresAt, err := c.exchange(ctx)
	if err != nil {
		// A failed early refresh must not cost the caller a token that still works.
		if token, expiresAt, ok := c.unexpired(); ok {
			return token, expiresAt, nil
		}

		return "", time.Time{}, err
	}

	return token, expiresAt, nil
}

func (c *Client) exchange(ctx context.Context) (string, time.Time, error) {
	body, err := json.Marshal(struct {
		Type string `json:"type"`
	}{Type: tokenType})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("marshal token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.vmToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("broker a Build Cache token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048)) //nolint:mnd

		return "", time.Time{}, fmt.Errorf("broker a Build Cache token: status %d: %s", //nolint:err113
			resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken string `json:"accessToken"`
		ExpiresAt   string `json:"expiresAt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", time.Time{}, fmt.Errorf("decode brokered token: %w", err)
	}
	if result.AccessToken == "" {
		return "", time.Time{}, errEmptyToken
	}

	// The issuer stamps the real expiry; trusting it rather than a local guess is
	// what lets callers refresh on the actual deadline.
	expiresAt, err := time.Parse(time.RFC3339, result.ExpiresAt)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("parse brokered token expiry %q: %w", result.ExpiresAt, err)
	}

	c.store(result.AccessToken, expiresAt)

	return result.AccessToken, expiresAt, nil
}

func (c *Client) cached() (string, time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.token != "" && time.Now().Add(refreshSkew).Before(c.expiresAt) {
		return c.token, c.expiresAt, true
	}

	return "", time.Time{}, false
}

// unexpired is cached without the safety skew: close enough to expiry that we would
// rather exchange, but still better than failing the caller outright.
func (c *Client) unexpired() (string, time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.token != "" && time.Now().Before(c.expiresAt) {
		return c.token, c.expiresAt, true
	}

	return "", time.Time{}, false
}

func (c *Client) store(token string, expiresAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token, c.expiresAt = token, expiresAt
}
