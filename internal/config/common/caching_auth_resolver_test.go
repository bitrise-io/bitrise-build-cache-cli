//go:build unit

package common_test

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
)

func TestCachingAuthResolver_FirstGetCallsResolver(t *testing.T) {
	fresh := common.CacheAuthConfig{AuthToken: "fresh-tok", WorkspaceID: "ws-1"}
	calls := int32(0)
	resolver := func() (common.CacheAuthConfig, error) {
		atomic.AddInt32(&calls, 1)

		return fresh, nil
	}

	c := common.NewCachingAuthResolver(time.Minute, resolver, log.NewLogger())

	got := c.Get()

	assert.Equal(t, fresh, got)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestCachingAuthResolver_RefreshAfterTTL(t *testing.T) {
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

	c := common.NewCachingAuthResolver(10*time.Millisecond, resolver, log.NewLogger())

	assert.Equal(t, first, c.Get())

	time.Sleep(25 * time.Millisecond)

	got := c.Get()

	assert.Equal(t, second, got)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestCachingAuthResolver_SingleFlightPerTTL(t *testing.T) {
	calls := int32(0)
	resolver := func() (common.CacheAuthConfig, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(5 * time.Millisecond)

		return common.CacheAuthConfig{AuthToken: "fresh-tok", WorkspaceID: "ws-1"}, nil
	}

	c := common.NewCachingAuthResolver(time.Minute, resolver, log.NewLogger())

	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = c.Get()
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestCachingAuthResolver_ResolverErrorReturnsCached(t *testing.T) {
	fresh := common.CacheAuthConfig{AuthToken: "tok", WorkspaceID: "ws"}
	calls := int32(0)
	resolver := func() (common.CacheAuthConfig, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			return fresh, nil
		}

		return common.CacheAuthConfig{}, errors.New("boom")
	}

	c := common.NewCachingAuthResolver(5*time.Millisecond, resolver, log.NewLogger())

	assert.Equal(t, fresh, c.Get())

	time.Sleep(10 * time.Millisecond)

	assert.Equal(t, fresh, c.Get())
}

func TestCachingAuthResolver_LogsOnRefresh(t *testing.T) {
	fresh := common.CacheAuthConfig{AuthToken: "fresh-tok", WorkspaceID: "ws-1"}
	resolver := func() (common.CacheAuthConfig, error) {
		return fresh, nil
	}

	var out bytes.Buffer
	logger := log.NewLogger(log.WithOutput(&out))

	c := common.NewCachingAuthResolver(time.Minute, resolver, logger)

	assert.Equal(t, fresh, c.Get())
	assert.Contains(t, out.String(), "auth token refreshed via credential source")
}

func TestCachingAuthResolver_LogsOnResolverError(t *testing.T) {
	resolver := func() (common.CacheAuthConfig, error) {
		return common.CacheAuthConfig{}, errors.New("boom")
	}

	var out bytes.Buffer
	logger := log.NewLogger(log.WithOutput(&out))

	c := common.NewCachingAuthResolver(time.Minute, resolver, logger)

	_ = c.Get()

	assert.Contains(t, out.String(), "auth token refresh failed, using cached value")
}
