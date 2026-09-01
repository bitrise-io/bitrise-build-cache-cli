//go:build unit

package kv

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	remoteexecution "github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/build/bazel/remote/execution/v2"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/proto/kv_storage"
)

// buildChannels with no injected stubs dials one entry per channel using
// max(2, NumCPU/6). Each entry gets a throttling semaphore sized to NumCPU.
func TestBuildPool_DefaultSizing(t *testing.T) {
	channels, err := buildChannels(NewClientParams{
		UseInsecure: true,
		Host:        "localhost:0",
		AuthConfig:  auth.Credential{Token: "tok"},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		for _, e := range channels {
			if e.conn != nil {
				_ = e.conn.Close()
			}
		}
	})

	assert.Len(t, channels, numChannels())
	for _, e := range channels {
		require.NotNil(t, e.conn)
		require.NotNil(t, e.sem)
		assert.Equal(t, runtime.NumCPU(), cap(e.sem))
		assert.NotNil(t, e.bitriseKVClient)
		assert.NotNil(t, e.capabilitiesClient)
		assert.NotNil(t, e.casClient)
	}
}

// pickChannel hands entries out round-robin across concurrent callers.
// Over 1000 evenly-distributed picks on 4 goroutines with a 4-entry channels
// each entry should see ~250 hits.
func TestClient_PickEntry_RoundRobinConcurrent(t *testing.T) {
	const (
		poolSize   = 4
		callsTotal = 1000
		goroutines = 4
	)
	channels := make([]*channel, poolSize)
	for i := range channels {
		channels[i] = &channel{}
	}
	c := &Client{channels: channels}

	counts := make([]atomic.Int64, poolSize)
	callsPerG := callsTotal / goroutines

	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range callsPerG {
				e := c.pickChannel()
				for i := range channels {
					if channels[i] == e {
						counts[i].Add(1)

						break
					}
				}
			}
		}()
	}
	wg.Wait()

	for i := range counts {
		got := counts[i].Load()
		assert.GreaterOrEqualf(t, got, int64(200), "entry %d saw %d calls, want >= 200", i, got)
		assert.LessOrEqualf(t, got, int64(300), "entry %d saw %d calls, want <= 300", i, got)
	}
}

// Injecting BitriseKVClient (or CapabilitiesClient) collapses the channels to one
// entry with no semaphore, so pickChannel never blocks — even under load that
// would otherwise exhaust perChannelLimit.
func TestBuildPool_InjectedStubIsUnthrottled(t *testing.T) {
	channels, err := buildChannels(NewClientParams{
		BitriseKVClient: &kvStubClient{},
	})
	require.NoError(t, err)
	require.Len(t, channels, 1)
	assert.Nil(t, channels[0].sem)
	assert.Nil(t, channels[0].conn)

	c := &Client{channels: channels}

	// Many concurrent picks + acquires — the nil sem short-circuits, none block.
	var wg sync.WaitGroup
	for range 500 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := c.pickChannel()
			_ = e.acquire(context.Background())
			e.release()
		}()
	}
	wg.Wait()
}

// Minimal stub to satisfy the non-nil check in buildChannels without pulling in moq.
type kvStubClient struct {
	kv_storage.KVStorageClient
}

// Ensure the capabilities-only injection path also collapses the channels.
func TestBuildPool_InjectedCapabilitiesStubOnly(t *testing.T) {
	channels, err := buildChannels(NewClientParams{
		CapabilitiesClient: capStubClient{},
	})
	require.NoError(t, err)
	require.Len(t, channels, 1)
	assert.Nil(t, channels[0].sem)
}

type capStubClient struct {
	remoteexecution.CapabilitiesClient
}

// A saturated channel must not hand back a slot once the caller's deadline has
// passed: doing so opens a stream the caller has no time left to feed, which the
// server reports as "receive first message: DeadlineExceeded".
func TestChannelAcquire_GivesUpWhenContextIsDone(t *testing.T) {
	ch := &channel{sem: make(chan struct{}, 1)}
	require.NoError(t, ch.acquire(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := ch.acquire(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), time.Second, "must give up at the deadline, not block on")
}

// A freed slot is still handed to a waiter whose deadline has not passed.
func TestChannelAcquire_SucceedsWhenASlotFrees(t *testing.T) {
	ch := &channel{sem: make(chan struct{}, 1)}
	require.NoError(t, ch.acquire(context.Background()))

	go func() {
		time.Sleep(30 * time.Millisecond)
		ch.release()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	assert.NoError(t, ch.acquire(ctx))
}

// The floor is the part that matters: NumCPU/6 yields two channels on anything
// under 12 cores, and two is what starved a customer build on a 7-core VM.
func TestNumChannels_FloorIsFour(t *testing.T) {
	assert.GreaterOrEqual(t, numChannels(), 4)
	assert.Equal(t, runtime.NumCPU(), perChannelLimit())
}
