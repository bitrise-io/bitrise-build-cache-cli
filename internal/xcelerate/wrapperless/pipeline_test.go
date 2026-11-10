//go:build unit

package wrapperless_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

	tl := wrapperless.NewTimeline()
	alloc := wrapperless.NewAllocator("h", 42, 1_000)

	p := &wrapperless.Pipeline{
		HomeDir:   t.TempDir(),
		Timeline:  tl,
		Allocator: alloc,
		Proxy:     proxy,
		Emitter:   &wrapperless.Emitter{Client: putter, SlimBackoff: time.Millisecond, SlimRetryBudget: 1},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, p.Start(ctx))
	t.Cleanup(p.Close)

	tl.PushStart(wrapperless.Start{
		At:              time.Unix(1, 0),
		BuildRequestHex: "deadbeefdeadbeefdeadbeefdeadbeef",
		Action:          wrapperless.PrimaryActionBuild,
	})

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if len(proxy.snapshot()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	ids := proxy.snapshot()
	require.NotEmpty(t, ids)
	assert.NotEmpty(t, ids[0])

	calls := putter.snapshot()
	require.NotEmpty(t, calls, "slim PUT must land")
	assert.Equal(t, ids[0], calls[0].InvocationID)
}

// TestPipeline_SlimPUTCarriesStartAction asserts the slim phase's rendered
// command reflects the Start's action + scheme (via the Bucket passed to
// OnOpenInvoc), not a bare default "build" verb. Covers the regression
// where every slim row read "build [scheme]" even for archive / test / analyze.
func TestPipeline_SlimPUTCarriesStartAction(t *testing.T) {
	proxy := &fakeProxy{}
	putter := &recordingPutter{}

	tl := wrapperless.NewTimeline()
	alloc := wrapperless.NewAllocator("h", 42, 1_000)

	p := &wrapperless.Pipeline{
		HomeDir:   t.TempDir(),
		Timeline:  tl,
		Allocator: alloc,
		Proxy:     proxy,
		Emitter:   &wrapperless.Emitter{Client: putter, SlimBackoff: time.Millisecond, SlimRetryBudget: 1},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, p.Start(ctx))
	t.Cleanup(p.Close)

	// Drive an Archive Start — slim PUT must render "archive [Seek]", not "build [Seek]".
	tl.PushStart(wrapperless.Start{
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
