package proxy_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/proxy"
	sessionproto "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/llvm/session"
)

func TestInactivity_FiresSlimEmitAfterTimeout(t *testing.T) {
	emitter := &capturingEmitter{}
	p := newProxyForEmit(t, emitter)
	p.InactivityTimeout = 30 * time.Millisecond

	_, err := p.SetSession(context.Background(), &sessionproto.SetSessionRequest{InvocationId: "inv-idle"})
	require.NoError(t, err)

	p.TouchSession()

	assert.Eventually(t, func() bool { return len(emitter.captured()) == 1 }, 500*time.Millisecond, 10*time.Millisecond)

	calls := emitter.captured()
	require.Len(t, calls, 1)
	assert.Equal(t, "inv-idle", calls[0].meta.InvocationID)
}

func TestInactivity_TouchResetsTimer(t *testing.T) {
	emitter := &capturingEmitter{}
	p := newProxyForEmit(t, emitter)
	p.InactivityTimeout = 60 * time.Millisecond

	_, err := p.SetSession(context.Background(), &sessionproto.SetSessionRequest{InvocationId: "inv-active"})
	require.NoError(t, err)

	for range 5 {
		p.TouchSession()
		time.Sleep(30 * time.Millisecond)
	}

	assert.Empty(t, emitter.captured(), "timer must not fire while activity keeps refreshing")

	assert.Eventually(t, func() bool { return len(emitter.captured()) == 1 }, 500*time.Millisecond, 10*time.Millisecond)
}

func TestInactivity_SetSessionSwapCancelsPendingEmit(t *testing.T) {
	emitter := &capturingEmitter{}
	p := newProxyForEmit(t, emitter)
	p.InactivityTimeout = 40 * time.Millisecond

	_, err := p.SetSession(context.Background(), &sessionproto.SetSessionRequest{InvocationId: "inv-A"})
	require.NoError(t, err)
	p.TouchSession()

	_, err = p.SetSession(context.Background(), &sessionproto.SetSessionRequest{InvocationId: "inv-B"})
	require.NoError(t, err)
	p.TouchSession()

	time.Sleep(120 * time.Millisecond)

	calls := emitter.captured()
	require.Len(t, calls, 2, "one emit for A on swap, one for B on inactivity")
	assert.Equal(t, "inv-A", calls[0].meta.InvocationID)
	assert.Equal(t, "inv-B", calls[1].meta.InvocationID)
}

func TestInactivity_FlushStopsTimer(t *testing.T) {
	emitter := &capturingEmitter{}
	p := newProxyForEmit(t, emitter)
	p.InactivityTimeout = 40 * time.Millisecond

	_, err := p.SetSession(context.Background(), &sessionproto.SetSessionRequest{InvocationId: "inv-flush"})
	require.NoError(t, err)
	p.TouchSession()
	p.FlushCurrentSession(context.Background())

	time.Sleep(120 * time.Millisecond)

	assert.Len(t, emitter.captured(), 1, "flush should emit once; timer must not double-emit")
}

func TestInactivity_NoSessionNoTimer(t *testing.T) {
	emitter := &capturingEmitter{}
	p := newProxyForEmit(t, emitter)
	p.InactivityTimeout = 20 * time.Millisecond

	p.TouchSession()

	time.Sleep(80 * time.Millisecond)

	assert.Empty(t, emitter.captured())
}

func TestSetSession_ResetsLastActivity(t *testing.T) {
	emitter := &capturingEmitter{}
	p := newProxyForEmit(t, emitter)

	stale := time.Now().Add(-time.Hour)
	p.SetLastActivity(stale)

	_, err := p.SetSession(context.Background(), &sessionproto.SetSessionRequest{InvocationId: "inv-reset"})
	require.NoError(t, err)

	assert.True(t, p.LastActivity().IsZero(), "SetSession must zero lastActivity from any prior session")
}

func TestIsSessionServiceMethod(t *testing.T) {
	assert.True(t, proxy.IsSessionServiceMethod("/session.Session/SetSession"))
	assert.False(t, proxy.IsSessionServiceMethod("/llvmcas.CASDBService/GetValue"))
	assert.False(t, proxy.IsSessionServiceMethod(""))
}

func TestInactivityDuration_ZeroFallsBackToDefault(t *testing.T) {
	p := newProxyForEmit(t, &capturingEmitter{})

	// Field zero → default (10s per proxy.go).
	assert.Equal(t, 10*time.Second, p.InactivityDuration())
}

func TestInactivityDuration_ConfiguredValueWins(t *testing.T) {
	p := newProxyForEmit(t, &capturingEmitter{})
	p.InactivityTimeout = 250 * time.Millisecond

	assert.Equal(t, 250*time.Millisecond, p.InactivityDuration())
}
