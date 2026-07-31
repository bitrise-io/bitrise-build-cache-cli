//go:build unit

package kv

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A rejected token is never accepted on a retry, and the upload stream often
// only surfaces Unauthenticated when the stream is closed — so both shapes have
// to be recognised or every upload burns the whole retry budget.
func TestIsUnauthenticated(t *testing.T) {
	assert.False(t, isUnauthenticated(nil))
	assert.False(t, isUnauthenticated(errors.New("boom")))
	assert.False(t, isUnauthenticated(status.Error(codes.NotFound, "missing")))
	assert.False(t, isUnauthenticated(status.Error(codes.Unavailable, "later")))

	assert.True(t, isUnauthenticated(status.Error(codes.Unauthenticated, "token validation failed")))
	assert.True(t, isUnauthenticated(ErrCacheUnauthenticated))
	assert.True(t, isUnauthenticated(fmt.Errorf("close upload: %w", ErrCacheUnauthenticated)),
		"the sentinel has to be seen through wrapping")
}
