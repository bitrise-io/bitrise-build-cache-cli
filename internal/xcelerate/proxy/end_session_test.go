//go:build unit

package proxy_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	sessionproto "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/session"
)

func TestSetSession_EndSession_emitsSingleInvocation(t *testing.T) {
	emitter := &capturingEmitter{}
	p := newProxyForEmit(t, emitter)

	_, err := p.SetSession(context.Background(), &sessionproto.SetSessionRequest{InvocationId: "inv-end"})
	require.NoError(t, err)

	_, err = p.EndSession(context.Background(), &sessionproto.EndSessionRequest{InvocationId: "inv-end"})
	require.NoError(t, err)

	calls := emitter.captured()
	require.Len(t, calls, 1)
	assert.Equal(t, "inv-end", calls[0].meta.InvocationID)
}

func TestSetSession_EndSession_prevents_inactivity_double_emit(t *testing.T) {
	emitter := &capturingEmitter{}
	p := newProxyForEmit(t, emitter)
	p.InactivityTimeout = 30 * time.Millisecond

	_, err := p.SetSession(context.Background(), &sessionproto.SetSessionRequest{InvocationId: "inv-race"})
	require.NoError(t, err)

	p.TouchSession()

	_, err = p.EndSession(context.Background(), &sessionproto.EndSessionRequest{InvocationId: "inv-race"})
	require.NoError(t, err)

	time.Sleep(120 * time.Millisecond)

	assert.Len(t, emitter.captured(), 1, "EndSession must cancel the inactivity timer so it does not double-emit")
}

func TestSetSession_EndSession_usesSuppliedEndTime(t *testing.T) {
	emitter := &capturingEmitter{}
	p := newProxyForEmit(t, emitter)

	_, err := p.SetSession(context.Background(), &sessionproto.SetSessionRequest{InvocationId: "inv-end-time"})
	require.NoError(t, err)

	captured := emitter.captured()
	require.Empty(t, captured)

	// Pick an explicit "future" end-of-build wall clock to prove the RPC arg wins over time.Now().
	target := time.Now().Add(2500 * time.Millisecond).UnixMilli()

	_, err = p.EndSession(context.Background(), &sessionproto.EndSessionRequest{
		InvocationId:  "inv-end-time",
		EndTimeUnixMs: target,
	})
	require.NoError(t, err)

	calls := emitter.captured()
	require.Len(t, calls, 1)
	assert.Equal(t, target, calls[0].meta.EndTime.UnixMilli())
}

func TestEndSession_UnknownInvocation_NoOp(t *testing.T) {
	emitter := &capturingEmitter{}
	p := newProxyForEmit(t, emitter)

	_, err := p.SetSession(context.Background(), &sessionproto.SetSessionRequest{InvocationId: "inv-active"})
	require.NoError(t, err)

	resp, err := p.EndSession(context.Background(), &sessionproto.EndSessionRequest{InvocationId: "different-id"})
	require.NoError(t, err)
	assert.IsType(t, &emptypb.Empty{}, resp)

	assert.Empty(t, emitter.captured(), "mismatched invocation id must not flush the active session")
}

func TestEndSession_NoActiveSession_NoOp(t *testing.T) {
	emitter := &capturingEmitter{}
	p := newProxyForEmit(t, emitter)

	require.NotPanics(t, func() {
		_, err := p.EndSession(context.Background(), &sessionproto.EndSessionRequest{InvocationId: "nothing-here"})
		require.NoError(t, err)
	})

	assert.Empty(t, emitter.captured())
}
