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
