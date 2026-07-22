//go:build unit

package xcelerate_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate"
)

func TestAuthProvider_SeedReturnedWithoutResolver(t *testing.T) {
	seed := common.CacheAuthConfig{AuthToken: "seed-tok", WorkspaceID: "ws-1"}
	calls := int32(0)
	resolver := func() (common.CacheAuthConfig, error) {
		atomic.AddInt32(&calls, 1)

		return common.CacheAuthConfig{AuthToken: "fresh-tok", WorkspaceID: "ws-2"}, nil
	}

	p := xcelerate.NewAuthProvider(seed, resolver, time.Minute, log.NewLogger())

	got := p.Get()

	assert.Equal(t, seed, got)
	assert.Equal(t, int32(0), atomic.LoadInt32(&calls))
}

func TestAuthProvider_RefreshAfterTTL(t *testing.T) {
	seed := common.CacheAuthConfig{AuthToken: "seed-tok", WorkspaceID: "ws-1"}
	fresh := common.CacheAuthConfig{AuthToken: "fresh-tok", WorkspaceID: "ws-2"}
	calls := int32(0)
	resolver := func() (common.CacheAuthConfig, error) {
		atomic.AddInt32(&calls, 1)

		return fresh, nil
	}

	p := xcelerate.NewAuthProvider(seed, resolver, 10*time.Millisecond, log.NewLogger())

	assert.Equal(t, seed, p.Get())

	time.Sleep(25 * time.Millisecond)

	got := p.Get()

	assert.Equal(t, fresh, got)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestAuthProvider_SingleFlightPerTTL(t *testing.T) {
	seed := common.CacheAuthConfig{AuthToken: "seed-tok", WorkspaceID: "ws-1"}
	calls := int32(0)
	resolver := func() (common.CacheAuthConfig, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(5 * time.Millisecond)

		return common.CacheAuthConfig{AuthToken: "fresh-tok", WorkspaceID: "ws-2"}, nil
	}

	p := xcelerate.NewAuthProvider(seed, resolver, 5*time.Millisecond, log.NewLogger())

	// Force expiration of the seed window.
	time.Sleep(10 * time.Millisecond)

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

func TestAuthProvider_ResolverErrorKeepsCached(t *testing.T) {
	seed := common.CacheAuthConfig{AuthToken: "seed-tok", WorkspaceID: "ws-1"}
	resolver := func() (common.CacheAuthConfig, error) {
		return common.CacheAuthConfig{}, errors.New("boom")
	}

	p := xcelerate.NewAuthProvider(seed, resolver, 5*time.Millisecond, log.NewLogger())

	time.Sleep(10 * time.Millisecond)

	got := p.Get()

	require.Equal(t, seed, got)
}
