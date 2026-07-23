//go:build unit

package xcelerate_test

import (
	"bytes"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate"
)

func TestAuthProvider_FirstGetCallsResolver(t *testing.T) {
	fresh := common.CacheAuthConfig{AuthToken: "fresh-tok", WorkspaceID: "ws-1"}
	calls := int32(0)
	resolver := func() (common.CacheAuthConfig, error) {
		atomic.AddInt32(&calls, 1)

		return fresh, nil
	}

	p := xcelerate.NewAuthProvider(resolver, time.Minute, log.NewLogger())

	got := p.Get()

	assert.Equal(t, fresh, got)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestAuthProvider_RefreshAfterTTL(t *testing.T) {
	first := common.CacheAuthConfig{AuthToken: "tok-1", WorkspaceID: "ws-1"}
	second := common.CacheAuthConfig{AuthToken: "tok-2", WorkspaceID: "ws-2"}
	calls := int32(0)
	resolver := func() (common.CacheAuthConfig, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			return first, nil
		}

		return second, nil
	}

	p := xcelerate.NewAuthProvider(resolver, 10*time.Millisecond, log.NewLogger())

	assert.Equal(t, first, p.Get())

	time.Sleep(25 * time.Millisecond)

	got := p.Get()

	assert.Equal(t, second, got)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestAuthProvider_SingleFlightPerTTL(t *testing.T) {
	calls := int32(0)
	resolver := func() (common.CacheAuthConfig, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(5 * time.Millisecond)

		return common.CacheAuthConfig{AuthToken: "fresh-tok", WorkspaceID: "ws-1"}, nil
	}

	p := xcelerate.NewAuthProvider(resolver, time.Minute, log.NewLogger())

	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = p.Get()
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestAuthProvider_ResolverErrorReturnsCached(t *testing.T) {
	fresh := common.CacheAuthConfig{AuthToken: "tok", WorkspaceID: "ws"}
	calls := int32(0)
	resolver := func() (common.CacheAuthConfig, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			return fresh, nil
		}

		return common.CacheAuthConfig{}, errors.New("boom")
	}

	p := xcelerate.NewAuthProvider(resolver, 5*time.Millisecond, log.NewLogger())

	assert.Equal(t, fresh, p.Get())

	time.Sleep(10 * time.Millisecond)

	assert.Equal(t, fresh, p.Get())
}

func TestAuthProvider_LogsGreppableLineOnRefresh(t *testing.T) {
	fresh := common.CacheAuthConfig{AuthToken: "fresh-tok", WorkspaceID: "ws-1"}
	resolver := func() (common.CacheAuthConfig, error) {
		return fresh, nil
	}

	var out bytes.Buffer
	logger := log.NewLogger(log.WithOutput(&out))

	p := xcelerate.NewAuthProvider(resolver, time.Minute, logger)

	assert.Equal(t, fresh, p.Get())

	// Greppable line used by the feature-e2e-xcelerate-auth-refresh workflow.
	assert.Contains(t, out.String(), "xcelerate auth token refreshed via credential source")
}

func TestAuthProvider_LogsGreppableLineOnResolverError(t *testing.T) {
	resolver := func() (common.CacheAuthConfig, error) {
		return common.CacheAuthConfig{}, errors.New("boom")
	}

	var out bytes.Buffer
	logger := log.NewLogger(log.WithOutput(&out))

	p := xcelerate.NewAuthProvider(resolver, time.Minute, logger)

	_ = p.Get()

	assert.Contains(t, out.String(), "xcelerate auth token refresh failed, using cached value")
}
