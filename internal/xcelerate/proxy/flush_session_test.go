package proxy_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestFlushSession_NilHookReturnsEmptyList(t *testing.T) {
	p := newProxyForEmit(t, nil)

	resp, err := p.FlushSession(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Empty(t, resp.GetEmittedInvocationIds())
}

func TestFlushSession_HookIDsFlowBackToResponse(t *testing.T) {
	p := newProxyForEmit(t, nil)
	p.FlushHook = func(_ context.Context) []string { return []string{"inv-a", "inv-b"} }

	resp, err := p.FlushSession(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Equal(t, []string{"inv-a", "inv-b"}, resp.GetEmittedInvocationIds())
}
