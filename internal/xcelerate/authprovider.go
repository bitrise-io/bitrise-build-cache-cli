package xcelerate

import (
	"sync"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

// AuthResolver returns a fresh CacheAuthConfig or an error. Called at most once
// per TTL window by AuthProvider.
type AuthResolver func() (common.CacheAuthConfig, error)

// AuthProvider caches a CacheAuthConfig for TTL and refreshes on demand. On
// resolver error the previously cached value is returned so an outage doesn't
// break in-flight RPCs.
//
// Concurrency: readers hit an RLock fast path; a single writer holds the write
// lock across a resolver call while other callers wait — single-flight per TTL
// window.
type AuthProvider struct {
	resolver AuthResolver
	ttl      time.Duration
	logger   log.Logger

	mu        sync.RWMutex
	cached    common.CacheAuthConfig
	seeded    bool
	fetchedAt time.Time
	nowFn     func() time.Time
}

// NewAuthProvider constructs a provider that fetches lazily on first Get.
func NewAuthProvider(resolver AuthResolver, ttl time.Duration, logger log.Logger) *AuthProvider {
	return &AuthProvider{
		resolver: resolver,
		ttl:      ttl,
		logger:   logger,
		nowFn:    time.Now,
	}
}

// Get returns a cached CacheAuthConfig, refreshing if stale.
func (p *AuthProvider) Get() common.CacheAuthConfig {
	p.mu.RLock()
	if p.seeded && p.nowFn().Sub(p.fetchedAt) < p.ttl {
		cfg := p.cached
		p.mu.RUnlock()

		return cfg
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.seeded && p.nowFn().Sub(p.fetchedAt) < p.ttl {
		return p.cached
	}

	if p.resolver == nil {
		return p.cached
	}

	fresh, err := p.resolver()
	if err != nil {
		if p.logger != nil {
			p.logger.Warnf("xcelerate auth token refresh failed, using cached value: %s", err)
		}
		p.fetchedAt = p.nowFn()

		return p.cached
	}

	p.cached = fresh
	p.seeded = true
	p.fetchedAt = p.nowFn()

	if p.logger != nil {
		// CI: asserted by feature-e2e-xcelerate-auth-refresh workflow
		p.logger.Infof("xcelerate auth token refreshed via credential source")
	}

	return p.cached
}
