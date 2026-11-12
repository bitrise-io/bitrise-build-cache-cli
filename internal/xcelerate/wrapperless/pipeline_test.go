//go:build unit

package wrapperless_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/wrapperless"
)

type fakeProxy struct {
	mu sync.Mutex
	ids []string
}

func (f *fakeProxy) SetCurrentBucketID(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids = append(f.ids, id)
}

func (f *fakeProxy) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]string, len(f.ids))
	copy(out, f.ids)

	return out
}

func TestPipeline_StartOpensReseatsAndEmits(t *testing.T) {
	proxy := &fakeProxy{}
	putter := &recordingPutter{}

	alloc := wrapperless.NewAllocator("h", 42, 1_000)

	p := &wrapperless.Pipeline{
		HomeDir:   t.TempDir(),
		Allocator: alloc,
		Proxy:     proxy,
		Emitter:   &wrapperless.Emitter{Client: putter, DrainInterval: time.Hour},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, p.Start(ctx))
	t.Cleanup(p.Close)

	alloc.PushStart(wrapperless.Start{
		At:              time.Unix(1, 0),
		BuildRequestHex: "deadbeefdeadbeefdeadbeefdeadbeef",
		Action:          wrapperless.PrimaryActionBuild,
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(putter.snapshot()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	ids := proxy.snapshot()
	require.NotEmpty(t, ids)
	// Pipeline opens a spec on Init to keep CurrentBucketID live; the Start
	// then supersedes it. Slim must fire on the Start-opened bucket.
	calls := putter.snapshot()
	require.NotEmpty(t, calls, "slim PUT must land")
	assert.Equal(t, ids[len(ids)-1], calls[0].InvocationID)
}

// TestPipeline_SlimPUTCarriesStartAction asserts the slim phase's rendered
// command reflects the Start's action + scheme (via the Bucket passed to
// OnOpenInvoc), not a bare default "build" verb. Covers the regression
// where every slim row read "build [scheme]" even for archive / test / analyze.
func TestPipeline_SlimPUTCarriesStartAction(t *testing.T) {
	proxy := &fakeProxy{}
	putter := &recordingPutter{}

	alloc := wrapperless.NewAllocator("h", 42, 1_000)

	p := &wrapperless.Pipeline{
		HomeDir:   t.TempDir(),
		Allocator: alloc,
		Proxy:     proxy,
		Emitter:   &wrapperless.Emitter{Client: putter, DrainInterval: time.Hour},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, p.Start(ctx))
	t.Cleanup(p.Close)

	// Drive an Archive Start — slim PUT must render "archive [Seek]", not "build [Seek]".
	alloc.PushStart(wrapperless.Start{
		At:              time.Unix(1, 0),
		BuildRequestHex: "feedfacefeedfacefeedfacefeedface",
		Action:          wrapperless.PrimaryActionArchive,
	})

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if len(putter.snapshot()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	calls := putter.snapshot()
	require.NotEmpty(t, calls)
	// Scheme is empty because Start has no scheme — the bare verb "archive"
	// is the actual slim-phase payload. The regression fixed by S5: without
	// it this read "build".
	assert.Equal(t, "archive", calls[0].Command, "slim PUT must carry Start.Action, not default to build")
}

type blockingPutter struct {
	mu    sync.Mutex
	block chan struct{}
	calls []analytics.Invocation
}

func (b *blockingPutter) PutInvocation(inv analytics.Invocation) error {
	<-b.block
	b.mu.Lock()
	b.calls = append(b.calls, inv)
	b.mu.Unlock()

	return nil
}

func (b *blockingPutter) snapshot() []analytics.Invocation {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]analytics.Invocation, len(b.calls))
	copy(out, b.calls)

	return out
}

// TestPipeline_SlowPUTDoesNotBlockProxyStamp asserts the proxy's
// CurrentBucketID is reseated for the next build even while the previous
// build's enriched PUT is still in flight — i.e. HTTP latency does not stall
// the Allocator reader.
func TestPipeline_SlowPUTDoesNotBlockProxyStamp(t *testing.T) {
	proxy := &fakeProxy{}
	putter := &blockingPutter{block: make(chan struct{})}

	alloc := wrapperless.NewAllocator("h", 42, 1_000)

	p := &wrapperless.Pipeline{
		HomeDir:   t.TempDir(),
		Allocator: alloc,
		Proxy:     proxy,
		Emitter:   &wrapperless.Emitter{Client: putter, DrainInterval: time.Hour},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, p.Start(ctx))
	t.Cleanup(func() {
		close(putter.block)
		p.Close()
	})

	// Build 1 Start + End → enriched PUT enters worker queue, blocks on
	// putter.block. Allocator reader must remain free to drain the next event.
	alloc.PushStart(wrapperless.Start{
		At:              time.Unix(1, 0),
		BuildRequestHex: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Action:          wrapperless.PrimaryActionBuild,
	})
	alloc.PushEnd(wrapperless.End{
		At:               time.Unix(2, 0),
		Scheme:           "Build1",
		Action:           wrapperless.PrimaryActionBuild,
		UniqueIdentifier: "end-1",
		Duration:         time.Second,
		Success:          true,
	})

	// Build 2 Start lands behind the slow PUT.
	alloc.PushStart(wrapperless.Start{
		At:              time.Unix(3, 0),
		BuildRequestHex: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Action:          wrapperless.PrimaryActionBuild,
	})

	// Wait for the proxy to see three stamps:
	//   1. initial spec (Init)
	//   2. build 1 real bucket
	//   3. spec opened after build 1 End OR build 2 real bucket
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if len(proxy.snapshot()) >= 3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	ids := proxy.snapshot()
	require.GreaterOrEqual(t, len(ids), 3, "proxy must be reseated for build 2 while enriched PUT is still blocked")
}
