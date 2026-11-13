// Package buildhub exchanges the per-VM token a Bitrise Build Hub runner or Remote
// Dev Env carries for a short-lived, workspace-scoped Build Cache token. It sits at L3 beside
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
	"strings"
	"sync"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/trustedurl"
)

// tokenType selects the Build Cache token. It must match the VMTokenType enum
// name in the instance-manager API.
const tokenType = "VM_TOKEN_TYPE_BUILD_CACHE" //nolint:gosec // enum name, not a credential

// refreshSkew is how long before expiry a token counts as stale, so a build step
// never starts with one that expires underneath it.
const refreshSkew = 5 * time.Minute

// requestTimeout bounds the exchange. This sits in front of every resolve on a
// VM that offers a token, so a hanging instance-manager must not hang the build.
const requestTimeout = 10 * time.Second

// failureBackoff stops a down instance-manager from costing every per-RPC resolve
// another requestTimeout before it falls through to the stores.
const failureBackoff = 30 * time.Second

var errEmptyToken = errors.New("instance-manager returned an empty token")

// Client exchanges the VM token for a Build Cache token and caches the result
// until it approaches expiry. It is safe for concurrent use.
type Client struct {
	tokenURL   string
	vmToken    string
	httpClient *http.Client
	now        func() time.Time

	// refreshSlot serialises exchanges so a burst of per-RPC resolves triggers one
	// request rather than one each. A channel rather than a mutex so waiting honours
	// the caller's context.
	refreshSlot chan struct{}
	mu          sync.RWMutex
	token       string
	expiresAt   time.Time
	lastErr     error
	lastErrAt   time.Time
}

// FromEnv builds a Client from the first usable pair, the Build Hub one first. A
// pair is usable when both halves are set and the URL is a trusted https one; halves
// are never combined across pairs.
func FromEnv(envs map[string]string) (*Client, bool) {
	p, ok := vmTokenPair(envs)
	if !ok {
		return nil, false
	}

	return &Client{
		tokenURL:    p.tokenURL,
		vmToken:     p.vmToken,
		httpClient:  &http.Client{Timeout: requestTimeout},
		now:         time.Now,
		refreshSlot: make(chan struct{}, 1),
		mu:          sync.RWMutex{},
		token:       "",
		expiresAt:   time.Time{},
		lastErr:     nil,
		lastErrAt:   time.Time{},
	}, true
}

// TokenEnv names the env var holding the VM token FromEnv would exchange, or "".
func TokenEnv(envs map[string]string) string {
	p, ok := vmTokenPair(envs)
	if !ok {
		return ""
	}

	return p.tokenEnv
}

type pair struct {
	tokenEnv, vmToken, tokenURL string
}

func vmTokenPair(envs map[string]string) (pair, bool) {
	for _, keys := range [...][2]string{
		{auth.EnvBuildHubVMToken, auth.EnvBuildHubVMTokenURL},
		{auth.EnvVMToken, auth.EnvVMTokenURL},
	} {
		vmToken := strings.TrimSpace(envs[keys[0]])
		// The VM token is sent to this URL, so one outside Bitrise's domains is not a broker.
		tokenURL := trustedurl.FromEnv(envs, keys[1], "https")
		if vmToken != "" && tokenURL != "" {
			return pair{tokenEnv: keys[0], vmToken: vmToken, tokenURL: tokenURL}, true
		}
	}

	return pair{}, false
}

// Process-wide, so every resolver in one command shares a single cached exchange.
var shared sync.Map //nolint:gochecknoglobals

func Shared(envs map[string]string) (*Client, bool) {
	p, _ := vmTokenPair(envs)
	key := p.tokenURL + "\x00" + p.vmToken
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

	token, expiresAt, err := c.exchangeUnlessBackingOff(ctx)
	if err == nil {
		return token, expiresAt, nil
	}

	// A failed early refresh must not cost the caller a token that still works.
	if token, expiresAt, ok := c.unexpired(); ok {
		return token, expiresAt, nil
	}

	return "", time.Time{}, err
}

// A caller that cancelled, or was done before the request went out, says nothing about
// instance-manager; one whose deadline the exchange outlasted does: exempting that too
// would let a hanging instance-manager cost every short-deadline caller the full wait.
func (c *Client) exchangeUnlessBackingOff(ctx context.Context) (string, time.Time, error) {
	if err := c.recentFailure(); err != nil {
		return "", time.Time{}, fmt.Errorf("backing off after previous failure: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", time.Time{}, fmt.Errorf("broker a Build Cache token: %w", err)
	}

	token, expiresAt, err := c.exchange(ctx)
	if err != nil && !errors.Is(ctx.Err(), context.Canceled) {
		c.recordFailure(err)
	}

	return token, expiresAt, err
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
