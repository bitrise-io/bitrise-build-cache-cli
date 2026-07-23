//go:build unit

package common

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
)

func TestCachingAuthResolver_ResolverErrorRetriesNextCall(t *testing.T) {
	calls := int32(0)
	resolver := func() (CacheAuthConfig, error) {
		atomic.AddInt32(&calls, 1)

		return CacheAuthConfig{}, errors.New("boom")
	}

	now := time.Now()
	c := NewCachingAuthResolver(time.Hour, resolver, log.NewLogger())
	c.nowFn = func() time.Time { return now }

	_ = c.Get()
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))

	now = now.Add(time.Second) // well within TTL

	_ = c.Get()
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls), "resolver must be retried immediately after an error, not throttled by TTL")
}
