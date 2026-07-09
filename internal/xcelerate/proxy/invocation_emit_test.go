package proxy_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/xcelerate/proxy"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/xcelerate/proxy/mocks"
	sessionproto "github.com/bitrise-io/bitrise-build-cache-cli/v2/proto/llvm/session"
)

type capturingEmitter struct {
	mu    sync.Mutex
	calls []capturedEmit
}

type capturedEmit struct {
	meta  proxy.SessionMeta
	stats proxy.SessionStats
}

func (e *capturingEmitter) EmitSlim(_ context.Context, meta proxy.SessionMeta, stats proxy.SessionStats) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.calls = append(e.calls, capturedEmit{meta: meta, stats: stats})
}

func (e *capturingEmitter) captured() []capturedEmit {
	e.mu.Lock()
	defer e.mu.Unlock()

	out := make([]capturedEmit, len(e.calls))
	copy(out, e.calls)

	return out
}

func newProxyForEmit(t *testing.T, emitter proxy.InvocationEmitter) *proxy.Proxy {
	t.Helper()

	kvClient := &mocks.ClientMock{}
	loggerFactory := func(string) (log.Logger, error) { return mockLogger, nil }

	return proxy.NewProxy(kvClient, false, mockLogger, loggerFactory, emitter)
}

func TestSetSession_replacingSessionEmitsPrior(t *testing.T) {
	emitter := &capturingEmitter{}
	p := newProxyForEmit(t, emitter)

	_, err := p.SetSession(context.Background(), &sessionproto.SetSessionRequest{
		InvocationId: "inv-1", AppSlug: "app", BuildSlug: "b1", StepSlug: "s1",
	})
	require.NoError(t, err)

	_, err = p.SetSession(context.Background(), &sessionproto.SetSessionRequest{
		InvocationId: "inv-2", AppSlug: "app", BuildSlug: "b2", StepSlug: "s2",
	})
	require.NoError(t, err)

	calls := emitter.captured()
	require.Len(t, calls, 1, "second SetSession should emit the first session")
	assert.Equal(t, "inv-1", calls[0].meta.InvocationID)
	assert.Equal(t, "b1", calls[0].meta.BuildSlug)
	assert.False(t, calls[0].meta.StartTime.IsZero(), "start time captured on SetSession")
}

func TestFlushCurrentSession_emitsOpenSession(t *testing.T) {
	emitter := &capturingEmitter{}
	p := newProxyForEmit(t, emitter)

	_, err := p.SetSession(context.Background(), &sessionproto.SetSessionRequest{
		InvocationId: "inv-3", AppSlug: "app", BuildSlug: "b3", StepSlug: "s3",
	})
	require.NoError(t, err)

	p.FlushCurrentSession(context.Background())

	calls := emitter.captured()
	require.Len(t, calls, 1)
	assert.Equal(t, "inv-3", calls[0].meta.InvocationID)
}

func TestFlushCurrentSession_noOpWhenNoSession(t *testing.T) {
	emitter := &capturingEmitter{}
	p := newProxyForEmit(t, emitter)

	p.FlushCurrentSession(context.Background())

	assert.Empty(t, emitter.captured())
}

func TestFlushCurrentSession_noOpWhenEmitterNil(t *testing.T) {
	p := newProxyForEmit(t, nil)

	_, err := p.SetSession(context.Background(), &sessionproto.SetSessionRequest{
		InvocationId: "inv-4",
	})
	require.NoError(t, err)

	assert.NotPanics(t, func() { p.FlushCurrentSession(context.Background()) })
}

func TestFlushCurrentSession_secondCallIsNoOp(t *testing.T) {
	emitter := &capturingEmitter{}
	p := newProxyForEmit(t, emitter)

	_, err := p.SetSession(context.Background(), &sessionproto.SetSessionRequest{InvocationId: "inv-5"})
	require.NoError(t, err)

	p.FlushCurrentSession(context.Background())
	p.FlushCurrentSession(context.Background())

	assert.Len(t, emitter.captured(), 1)
}

func TestSessionStats_HitRate_prefersKVWhenBothPresent(t *testing.T) {
	s := proxy.SessionStats{Hits: 1, Misses: 9, KVHits: 4, KVMisses: 1}
	assert.InDelta(t, 0.8, s.HitRate(), 0.0001)
}

func TestSessionStats_HitRate_fallsBackToBlob(t *testing.T) {
	s := proxy.SessionStats{Hits: 3, Misses: 1}
	assert.InDelta(t, 0.75, s.HitRate(), 0.0001)
}

func TestSessionStats_HitRate_zeroWhenNoOps(t *testing.T) {
	assert.InDelta(t, 0, proxy.SessionStats{}.HitRate(), 0.0001)
}

func TestSetSession_startTimeMonotonicish(t *testing.T) {
	emitter := &capturingEmitter{}
	p := newProxyForEmit(t, emitter)

	before := time.Now()
	_, err := p.SetSession(context.Background(), &sessionproto.SetSessionRequest{InvocationId: "inv-6"})
	require.NoError(t, err)
	after := time.Now()

	p.FlushCurrentSession(context.Background())

	calls := emitter.captured()
	require.Len(t, calls, 1)
	assert.False(t, calls[0].meta.StartTime.Before(before))
	assert.False(t, calls[0].meta.StartTime.After(after))
}

var _ = emptypb.Empty{}
