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

// TakeSnapshot returns the currently-set snapshot and clears it in one
// locked region, matching the real BlobStatsSource contract.
func (f *fakeBlobSource) TakeSnapshot() *blobstats.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	snap := f.snap
	f.snap = nil

	return snap
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

func TestAllocator_EndWithoutStart_SubBuild_Drops(t *testing.T) {
	var buckets []Bucket
	a, _ := newTestAllocator(t, func(b Bucket) { buckets = append(buckets, b) })

	a.Handle(TimelineEvent{Kind: EventKindEnd, End: End{UniqueIdentifier: "uid-sub-01", Scheme: "Signing"}})

	assert.Empty(t, buckets)
}

func TestAllocator_EndWithoutStart_PrimaryAction_MintsBucket(t *testing.T) {
	var buckets []Bucket
	a, blobs := newTestAllocator(t, func(b Bucket) { buckets = append(buckets, b) })
	blobs.set(42)

	ids := make(chan string, 1)
	a.OnOpenInvoc = func(b Bucket) { ids <- b.InvocationID }

	a.Handle(TimelineEvent{Kind: EventKindEnd, End: End{
		At:               time.Unix(100, 0),
		Scheme:           "IceCubesApp",
		Project:          "IceCubesApp",
		Configuration:    "Debug",
		Action:           PrimaryActionBuild,
		UniqueIdentifier: "ABCD-EFGH",
		Duration:         5 * time.Second,
		Success:          true,
	}})

	require.Len(t, buckets, 1)
	b := buckets[0]
	assert.Equal(t, PrimaryActionBuild, b.Action)
	assert.Equal(t, "IceCubesApp", b.Scheme)
	assert.Equal(t, "Debug", b.Configuration)
	assert.True(t, b.Success)
	assert.Equal(t, BucketReasonEndMatched, b.Reason)
	assert.NotEmpty(t, b.InvocationID)
	require.NotNil(t, b.BlobStats)
	assert.Equal(t, int64(42), b.BlobStats.Download.OpCount)

	select {
	case id := <-ids:
		assert.Equal(t, b.InvocationID, id)
	default:
		t.Fatal("OnOpenInvoc not called for sole-End bucket")
	}
}

func TestAllocator_StartThenEnd_EndActionOverridesStartAction(t *testing.T) {
	var buckets []Bucket
	a, _ := newTestAllocator(t, func(b Bucket) { buckets = append(buckets, b) })

	a.Handle(TimelineEvent{Kind: EventKindStart, Start: Start{
		At:              time.Unix(10, 0),
		BuildRequestHex: "0123456789abcdef0123456789abcdef",
		Action:          PrimaryActionBuild,
	}})
	a.Handle(TimelineEvent{Kind: EventKindEnd, End: End{
		At:               time.Unix(20, 0),
		Scheme:           "IceCubesApp",
		Action:           PrimaryActionAnalyze,
		UniqueIdentifier: "UID-ANALYZE",
		Success:          true,
	}})

	require.Len(t, buckets, 1)
	assert.Equal(t, PrimaryActionAnalyze, buckets[0].Action)
}

func TestAllocator_StartThenEnd_EmptyEndActionKeepsStartAction(t *testing.T) {
	var buckets []Bucket
	a, _ := newTestAllocator(t, func(b Bucket) { buckets = append(buckets, b) })

	a.Handle(TimelineEvent{Kind: EventKindStart, Start: Start{
		At:              time.Unix(10, 0),
		BuildRequestHex: "0123456789abcdef0123456789abcdef",
		Action:          PrimaryActionBuild,
	}})
	a.Handle(TimelineEvent{Kind: EventKindEnd, End: End{
		At:               time.Unix(20, 0),
		Scheme:           "MinimalApp",
		UniqueIdentifier: "UID-NO-ACTION",
	}})

	require.Len(t, buckets, 1)
	assert.Equal(t, PrimaryActionBuild, buckets[0].Action)
}

func TestAllocator_EndWithoutStart_EmptyUID_Drops(t *testing.T) {
	var buckets []Bucket
	a, _ := newTestAllocator(t, func(b Bucket) { buckets = append(buckets, b) })

	a.Handle(TimelineEvent{Kind: EventKindEnd, End: End{Action: PrimaryActionBuild}})

	assert.Empty(t, buckets)
}

// TestAllocator_StartPairedEnd_CarriesXCActivityLogPath asserts the sibling
// log path threads through the Start-paired bucket so the enriched-emit path
// can read CompilationCacheMetrics.
func TestAllocator_StartPairedEnd_CarriesXCActivityLogPath(t *testing.T) {
	var buckets []Bucket
	a, _ := newTestAllocator(t, func(b Bucket) { buckets = append(buckets, b) })

	a.Handle(TimelineEvent{Kind: EventKindStart, Start: Start{
		At:              time.Unix(1, 0),
		BuildRequestHex: "aabbccddeeff00112233445566778899",
		Action:          PrimaryActionBuild,
	}})
	a.Handle(TimelineEvent{Kind: EventKindEnd, End: End{
		At:                time.Unix(2, 0),
		Scheme:            "Seek",
		Action:            PrimaryActionBuild,
		UniqueIdentifier:  "LOG-1",
		Duration:          1 * time.Second,
		XCActivityLogPath: "/dd/Logs/Build/LOG-1.xcactivitylog",
	}})

	require.Len(t, buckets, 1)
	assert.Equal(t, "/dd/Logs/Build/LOG-1.xcactivitylog", buckets[0].XCActivityLogPath)
}

// TestAllocator_SoleEndMint_CarriesXCActivityLogPath asserts the sibling log
// path also threads through the warm ⌘B path (no Start seen).
func TestAllocator_SoleEndMint_CarriesXCActivityLogPath(t *testing.T) {
	var buckets []Bucket
	a, _ := newTestAllocator(t, func(b Bucket) { buckets = append(buckets, b) })

	a.Handle(TimelineEvent{Kind: EventKindEnd, End: End{
		At:                time.Unix(100, 0),
		Scheme:            "IceCubesApp",
		Action:            PrimaryActionBuild,
		UniqueIdentifier:  "ABCD-EFGH",
		Duration:          5 * time.Second,
		Success:           true,
		XCActivityLogPath: "/dd/Logs/Build/ABCD-EFGH.xcactivitylog",
	}})

	require.Len(t, buckets, 1)
	assert.Equal(t, "/dd/Logs/Build/ABCD-EFGH.xcactivitylog", buckets[0].XCActivityLogPath)
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
	assert.Equal(t, AnchorSaltIDEBackground, "ide-background")
}

func TestAllocator_CLI_NoOpenBucketSynthesizesImplicitEND(t *testing.T) {
	var buckets []Bucket
	var mu sync.Mutex
	a, blobs := newTestAllocator(t, func(b Bucket) {
		mu.Lock()
		buckets = append(buckets, b)
		mu.Unlock()
	})

	blobs.set(2)

	a.Handle(TimelineEvent{Kind: EventKindQuit, Quit: Quit{
		At:          time.Unix(10, 0),
		PID:         7777,
		StartTimeMS: 123456,
	}})

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, buckets, 1)
	assert.Equal(t, BucketReasonCLIImplicit, buckets[0].Reason)
	assert.Equal(t, 7777, buckets[0].PeerPID)
	assert.Equal(t, int64(123456), buckets[0].PeerStartMS)
	assert.NotEmpty(t, buckets[0].InvocationID)
}

// TestAllocator_QuitIDE_StartDuringDebounceDoesNotEvictNewBucket covers the
// race where the IDE debounce timer's AfterFunc has already fired (and the
// timer goroutine is blocked waiting on the allocator lock) before a Start
// reseats the bucket. Without the StartAt-vs-q.At guard, the firing goroutine
// evicts the freshly-opened bucket as EndOfPrior even though the new build
// belongs to a different user action.
//
// Simulates the race deterministically by driving flushIDEBackground directly
// (as if its AfterFunc had just fired) against an already-open bucket whose
// StartAt is after the Quit's At.
func TestAllocator_QuitIDE_StartDuringDebounceDoesNotEvictNewBucket(t *testing.T) {
	var buckets []Bucket
	var mu sync.Mutex
	a, blobs := newTestAllocator(t, func(b Bucket) {
		mu.Lock()
		buckets = append(buckets, b)
		mu.Unlock()
	})

	blobs.set(5)

	quitAt := time.Unix(100, 0)

	// Seat an open bucket whose StartAt is after the Quit — mirrors the
	// state where onStart won the lock race while the AfterFunc was already
	// scheduled and about to fire.
	a.mu.Lock()
	a.open = &Bucket{ //nolint:exhaustruct
		InvocationID:    "fresh-after-quit",
		StartAt:         quitAt.Add(5 * time.Millisecond),
		BuildRequestHex: "freshhex",
		Action:          PrimaryActionBuild,
	}
	a.mu.Unlock()

	// Now drive flushIDEBackground directly — equivalent to the AfterFunc
	// firing right now. With the guard, the open bucket survives; without
	// it, flushIDEBackground emits an EndOfPrior.
	a.flushIDEBackground(Quit{At: quitAt, PID: 10, AncestryIDE: true})

	mu.Lock()
	defer mu.Unlock()
	for _, b := range buckets {
		assert.NotEqual(t, BucketReasonEndOfPrior, b.Reason,
			"IDE debounce firing after a newer Start must not evict the fresh bucket as EndOfPrior")
	}
}

// TestAllocator_IDEBackground_PeerStartMSParticipatesInID covers PID-recycle
// collision: two IDE Quits with the same PID but different StartTimeMS must
// derive distinct InvocationIDs so the second session cannot accidentally
// re-PUT under the first session's row.
func TestAllocator_IDEBackground_PeerStartMSParticipatesInID(t *testing.T) {
	var buckets []Bucket
	var mu sync.Mutex

	driveIDEQuit := func(startMS int64) {
		buckets = nil
		a, blobs := newTestAllocator(t, func(b Bucket) {
			mu.Lock()
			buckets = append(buckets, b)
			mu.Unlock()
		})

		blobs.set(1)
		a.Handle(TimelineEvent{Kind: EventKindQuit, Quit: Quit{
			At:          time.Unix(100, 0),
			PID:         777, // same PID both runs → recycle simulation
			StartTimeMS: startMS,
			AncestryIDE: true,
		}})

		deadline := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(deadline) {
			mu.Lock()
			n := len(buckets)
			mu.Unlock()
			if n > 0 {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	driveIDEQuit(1_000)
	mu.Lock()
	require.Len(t, buckets, 1)
	firstID := buckets[0].InvocationID
	mu.Unlock()

	driveIDEQuit(2_000)
	mu.Lock()
	require.Len(t, buckets, 1)
	secondID := buckets[0].InvocationID
	mu.Unlock()

	assert.NotEqual(t, firstID, secondID,
		"IDE-background buckets with same PID but different StartTimeMS must derive distinct InvocationIDs")
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
