package proxy_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/proxy"
)

func TestContextWithWorkspaceID_emptySlugIsNoOp(t *testing.T) {
	ctx := proxy.ContextWithWorkspaceID(context.Background(), "")
	_, ok := metadata.FromOutgoingContext(ctx)
	assert.False(t, ok, "empty workspace ID must not attach metadata")
}

func TestContextWithWorkspaceID_setsOutgoingMetadata(t *testing.T) {
	ctx := proxy.ContextWithWorkspaceID(context.Background(), "acme")
	md, ok := metadata.FromOutgoingContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, []string{"acme"}, md.Get("x-bitrise-workspace-id"))
}

func TestWorkspaceIDFromContext_readsIncomingMetadata(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-bitrise-workspace-id", "acme"))
	assert.Equal(t, "acme", proxy.WorkspaceIDFromContext(ctx))
}

func TestWorkspaceIDFromContext_absentReturnsEmpty(t *testing.T) {
	assert.Empty(t, proxy.WorkspaceIDFromContext(context.Background()))
	assert.Empty(t, proxy.WorkspaceIDFromContext(metadata.NewIncomingContext(context.Background(), metadata.Pairs())))
}
