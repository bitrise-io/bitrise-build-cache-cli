package common

import (
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

// AuthResolver returns a fresh CacheAuthConfig or an error. Called at most once
// per TTL window by CachingAuthResolver.
type AuthResolver func() (CacheAuthConfig, error)

// CachingAuthResolver caches a CacheAuthConfig for TTL and refreshes on demand.
// On resolver error the previously cached value is returned so a transient
// outage doesn't break in-flight RPCs; the next Get retries immediately rather
// than waiting a full TTL.
//
// Concurrency: readers hit an RLock fast path; a single writer holds the write
// lock across a resolver call while other callers wait — single-flight per TTL
// window.
type CachingAuthResolver struct {
	resolver AuthResolver
	ttl      time.Duration
	logger   log.Logger

	mu        sync.RWMutex
	cached    CacheAuthConfig
	seeded    bool
	fetchedAt time.Time
	nowFn     func() time.Time
}

// NewCachingAuthResolver wraps resolver with a TTL cache. Logger is optional.
func NewCachingAuthResolver(ttl time.Duration, resolver AuthResolver, logger log.Logger) *CachingAuthResolver {
	return &CachingAuthResolver{
		resolver: resolver,
		ttl:      ttl,
		logger:   logger,
		nowFn:    time.Now,
	}
}

// Get returns a cached CacheAuthConfig, refreshing lazily when stale.
func (c *CachingAuthResolver) Get() CacheAuthConfig {
	c.mu.RLock()
	if c.seeded && c.nowFn().Sub(c.fetchedAt) < c.ttl {
		cfg := c.cached
		c.mu.RUnlock()

		return cfg
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.seeded && c.nowFn().Sub(c.fetchedAt) < c.ttl {
		return c.cached
	}

	if c.resolver == nil {
		return c.cached
	}

	fresh, err := c.resolver()
	if err != nil {
		if c.logger != nil {
			c.logger.Warnf("auth token refresh failed, using cached value: %s", err)
		}

		return c.cached
	}

	c.cached = fresh
	c.seeded = true
	c.fetchedAt = c.nowFn()

	if c.logger != nil {
		c.logger.Infof("auth token refreshed via credential source")
	}

	return c.cached
}
