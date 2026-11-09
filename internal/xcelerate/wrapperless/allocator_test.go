//go:build unit

package wrapperless

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/blobstats"
)

type fakeBlobSource struct {
	mu   sync.Mutex
	snap *blobstats.Snapshot
}

func (f *fakeBlobSource) set(opCount int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snap = &blobstats.Snapshot{ //nolint:exhaustruct
		Download: blobstats.DirectionSnapshot{OpCount: opCount}, //nolint:exhaustruct
	}
}

func (f *fakeBlobSource) Snapshot() *blobstats.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.snap
}

func (f *fakeBlobSource) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snap = nil
}

func newTestAllocator(t *testing.T, onReady func(Bucket)) (*Allocator, *fakeBlobSource) {
	t.Helper()
	blobs := &fakeBlobSource{}
	a := NewAllocator("host", 1234, 5678)
	a.BlobStats = blobs
	a.OnReady = onReady
	a.IDEBackgroundTO = 25 * time.Millisecond

	return a, blobs
}

func TestAllocator_HappyPath_StartThenEndEmitsOne(t *testing.T) {
	var buckets []Bucket
	var mu sync.Mutex

	a, _ := newTestAllocator(t, func(b Bucket) {
		mu.Lock()
		buckets = append(buckets, b)
		mu.Unlock()
	})

	a.Handle(TimelineEvent{Kind: EventKindStart, Start: Start{
		At:              time.Unix(1, 0),
		BuildRequestHex: "aabbccddeeff00112233445566778899",
		Action:          PrimaryActionBuild,
	}})
	a.Handle(TimelineEvent{Kind: EventKindEnd, End: End{
		At:               time.Unix(2, 0),
		Scheme:           "Seek",
		Success:          true,
		UniqueIdentifier: "LOG-1",
		Duration:         1 * time.Second,
	}})

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, buckets, 1)
	b := buckets[0]
	assert.Equal(t, BucketReasonEndMatched, b.Reason)
	assert.Equal(t, "Seek", b.Scheme)
	assert.Equal(t, PrimaryActionBuild, b.Action)
	assert.NotEmpty(t, b.InvocationID)
}

func TestAllocator_StartStartClosesPriorAsEndOfPrior(t *testing.T) {
	var buckets []Bucket
	var mu sync.Mutex

	a, _ := newTestAllocator(t, func(b Bucket) {
		mu.Lock()
		buckets = append(buckets, b)
		mu.Unlock()
	})

	a.Handle(TimelineEvent{Kind: EventKindStart, Start: Start{
		At: time.Unix(1, 0), BuildRequestHex: "hex1", Action: PrimaryActionBuild,
	}})
	a.Handle(TimelineEvent{Kind: EventKindStart, Start: Start{
		At: time.Unix(2, 0), BuildRequestHex: "hex2", Action: PrimaryActionBuild,
	}})
	a.Handle(TimelineEvent{Kind: EventKindStart, Start: Start{
		At: time.Unix(3, 0), BuildRequestHex: "hex3", Action: PrimaryActionBuild,
	}})

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, buckets, 2, "each new Start without End closes the prior")
	assert.Equal(t, BucketReasonEndOfPrior, buckets[0].Reason)
	assert.Equal(t, BucketReasonEndOfPrior, buckets[1].Reason)
	assert.Equal(t, "hex1", buckets[0].BuildRequestHex)
	assert.Equal(t, "hex2", buckets[1].BuildRequestHex)
}

func TestAllocator_EndWithoutStartDoesNothing(t *testing.T) {
	var buckets []Bucket
	a, _ := newTestAllocator(t, func(b Bucket) { buckets = append(buckets, b) })

	a.Handle(TimelineEvent{Kind: EventKindEnd, End: End{}})

	assert.Empty(t, buckets)
}

func TestAllocator_QuitIDE_Debounces_ThenFlushesBackground(t *testing.T) {
	var buckets []Bucket
	var mu sync.Mutex
	a, blobs := newTestAllocator(t, func(b Bucket) {
		mu.Lock()
		buckets = append(buckets, b)
		mu.Unlock()
	})

	blobs.set(7)

	a.Handle(TimelineEvent{Kind: EventKindQuit, Quit: Quit{
		At:          time.Unix(10, 0),
		PID:         99,
		AncestryIDE: true,
	}})

	time.Sleep(10 * time.Millisecond)
	mu.Lock()
	seenBefore := len(buckets)
	mu.Unlock()
	assert.Zero(t, seenBefore, "IDE flush must debounce")

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(buckets)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, buckets, 1)
	assert.Equal(t, BucketReasonIDEBackground, buckets[0].Reason)
}

func TestAllocator_QuitCLI_FlushesImmediately(t *testing.T) {
	var buckets []Bucket
	var mu sync.Mutex
	a, blobs := newTestAllocator(t, func(b Bucket) {
		mu.Lock()
		buckets = append(buckets, b)
		mu.Unlock()
	})

	blobs.set(3)

	a.Handle(TimelineEvent{Kind: EventKindQuit, Quit: Quit{
		At:  time.Unix(20, 0),
		PID: 42,
	}})

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, buckets, 1)
	assert.Equal(t, BucketReasonCLIImplicit, buckets[0].Reason)
	assert.NotEmpty(t, buckets[0].InvocationID)
}

func TestAllocator_QuitIDE_EndDuringDebounceCancelsBackground(t *testing.T) {
	var buckets []Bucket
	var mu sync.Mutex
	a, blobs := newTestAllocator(t, func(b Bucket) {
		mu.Lock()
		buckets = append(buckets, b)
		mu.Unlock()
	})

	a.Handle(TimelineEvent{Kind: EventKindStart, Start: Start{
		At: time.Unix(1, 0), BuildRequestHex: "openhex", Action: PrimaryActionBuild,
	}})
	blobs.set(5)

	a.Handle(TimelineEvent{Kind: EventKindQuit, Quit: Quit{PID: 10, AncestryIDE: true}})

	time.Sleep(5 * time.Millisecond)
	a.Handle(TimelineEvent{Kind: EventKindEnd, End: End{
		At: time.Unix(2, 0), Scheme: "Seek", Success: true, Duration: 1 * time.Second,
	}})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, buckets, 1)
	assert.Equal(t, BucketReasonEndMatched, buckets[0].Reason, "End during debounce must cancel IDE-background flush")
}

func TestAllocator_IDEBackground_UsesFixedCommand(t *testing.T) {
	var buckets []Bucket
	var mu sync.Mutex
	a, blobs := newTestAllocator(t, func(b Bucket) {
		mu.Lock()
		buckets = append(buckets, b)
		mu.Unlock()
	})

	blobs.set(1)

	a.Handle(TimelineEvent{Kind: EventKindQuit, Quit: Quit{PID: 1, AncestryIDE: true}})

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := len(buckets)
		mu.Unlock()
		if got > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, buckets, 1)
	assert.Equal(t, BucketReasonIDEBackground, buckets[0].Reason)
	assert.Equal(t, BuildRequestHexIDEBackground, "ide-background")
}

func TestAllocator_IDEBackground_SkipsWhenNoActivity(t *testing.T) {
	var buckets []Bucket
	var mu sync.Mutex
	a, _ := newTestAllocator(t, func(b Bucket) {
		mu.Lock()
		buckets = append(buckets, b)
		mu.Unlock()
	})

	a.Handle(TimelineEvent{Kind: EventKindQuit, Quit: Quit{PID: 7, AncestryIDE: true}})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, buckets, "no activity + no open bucket → no IDE-background emission")
}
