//go:build unit

package xcelerate

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

func TestAuthProvider_ResolverErrorRetriesNextCall(t *testing.T) {
	calls := int32(0)
	resolver := func() (common.CacheAuthConfig, error) {
		atomic.AddInt32(&calls, 1)

		return common.CacheAuthConfig{}, errors.New("boom")
	}

	now := time.Now()
	p := NewAuthProvider(resolver, time.Hour, log.NewLogger())
	p.nowFn = func() time.Time { return now }

	_ = p.Get()
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))

	now = now.Add(time.Second) // well within TTL

	_ = p.Get()
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls), "resolver must be retried immediately after an error, not throttled by TTL")
}
