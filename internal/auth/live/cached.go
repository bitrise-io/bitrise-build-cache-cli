package live

import (
	"context"
	"sync"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

// HotPathTTL bounds how stale a long-lived process's credential gets without a resolve per request.
const HotPathTTL = time.Minute

// cachedExpirySkew keeps a cached credential from being served in its last minute.
const cachedExpirySkew = time.Minute

// Cached is Bound for a hot path: it re-resolves at most once per ttl, and sooner
// when the credential is about to expire. It satisfies kv.AuthSource structurally.
type Cached struct {
	bound *Bound
	ttl   time.Duration
	now   func() time.Time

	mu         sync.Mutex
	cred       auth.Credential
	resolvedAt time.Time
	refreshing chan struct{}
}

func (b *Bound) Cached(ttl time.Duration) *Cached {
	return &Cached{bound: b, ttl: ttl, now: time.Now}
}

// Get serves the last good credential when a re-resolve fails, rather than a zero one.
// One caller re-resolves; the rest keep the current credential while it is unexpired.
func (c *Cached) Get(ctx context.Context) auth.Credential {
	c.mu.Lock()
	now := c.now()
	if c.fresh(now) {
		defer c.mu.Unlock()

		return c.cred
	}

	if wait := c.refreshing; wait != nil {
		cred := c.cred
		c.mu.Unlock()
		if cred.Token != "" && (cred.Expiry.IsZero() || now.Before(cred.Expiry)) {
			return cred
		}

		select {
		case <-wait:
		case <-ctx.Done():
		}
		c.mu.Lock()
		defer c.mu.Unlock()

		return c.cred
	}

	done := make(chan struct{})
	c.refreshing = done
	c.mu.Unlock()

	cred, _, err := c.bound.Resolve(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshing = nil
	close(done)

	if err != nil {
		c.bound.resolver.debugf("serving the cached credential, resolve failed: %s", err)

		return c.cred
	}

	c.cred, c.resolvedAt = cred, c.now()

	return cred
}

func (c *Cached) fresh(now time.Time) bool {
	if c.cred.Token == "" || now.Sub(c.resolvedAt) >= c.ttl {
		return false
	}

	return c.cred.Expiry.IsZero() || now.Add(cachedExpirySkew).Before(c.cred.Expiry)
}
