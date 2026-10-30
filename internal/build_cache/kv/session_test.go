//go:build unit

package kv_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/build_cache/kv"
)

func TestWithInvocationID_RoundTrip(t *testing.T) {
	ctx := kv.WithInvocationID(context.Background(), "peer-123")

	got, ok := kv.InvocationIDFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, "peer-123", got)
}

func TestWithInvocationID_EmptyIsNoOp(t *testing.T) {
	base := context.Background()

	ctx := kv.WithInvocationID(base, "")

	_, ok := kv.InvocationIDFromContext(ctx)
	assert.False(t, ok)
}

func TestInvocationIDFromContext_MissingReturnsFalse(t *testing.T) {
	_, ok := kv.InvocationIDFromContext(context.Background())
	assert.False(t, ok)
}
