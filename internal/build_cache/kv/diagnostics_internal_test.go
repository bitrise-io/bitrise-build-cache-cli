//go:build unit

package kv

import (
	"context"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAcquire_TimeoutReportsOccupancy(t *testing.T) {
	ch := &channel{sem: make(chan struct{}, 2)}
	require.NoError(t, ch.acquire(context.Background()))
	require.NoError(t, ch.acquire(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := ch.acquire(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "2/2 slots busy")
	assert.Contains(t, err.Error(), "waited")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestAcquire_NoSemaphoreAlwaysSucceeds(t *testing.T) {
	ch := &channel{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.NoError(t, ch.acquire(ctx))
}

func TestPoolSnapshot_CountsAcrossChannels(t *testing.T) {
	c := &Client{channels: []*channel{
		{sem: make(chan struct{}, 3)},
		{sem: make(chan struct{}, 3)},
	}}
	require.NoError(t, c.channels[0].acquire(context.Background()))
	require.NoError(t, c.channels[1].acquire(context.Background()))
	require.NoError(t, c.channels[1].acquire(context.Background()))

	snap := c.poolSnapshot()

	assert.Equal(t, 2, snap.channels)
	assert.Equal(t, 6, snap.capacity)
	assert.Equal(t, 3, snap.inFlight)
	assert.Contains(t, snap.String(), "3/6 slots busy")
}

func TestSampleContention_IsPopulated(t *testing.T) {
	snap := sampleContention()

	assert.Positive(t, snap.goroutines)
	assert.Positive(t, snap.maxRSSBytes)
	assert.Contains(t, snap.String(), "sched_p99=")
	assert.Contains(t, snap.String(), "involuntary_switches=")
}

func TestSamplePool_StopsWhenClosed(t *testing.T) {
	c := &Client{channels: []*channel{{sem: make(chan struct{}, 1)}}, logger: log.NewLogger()}
	stop := make(chan struct{})

	done := make(chan struct{})
	go func() { c.samplePool(stop); close(done) }()
	close(stop)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("samplePool did not return after stop was closed")
	}
}

type countingLogger struct {
	log.Logger
	warns int
}

func (l *countingLogger) Warnf(string, ...any) { l.warns++ }

// A saturated pool fails thousands of operations; the log must not carry one
// line each.
func TestLogContention_RateLimited(t *testing.T) {
	lg := &countingLogger{Logger: log.NewLogger()}
	c := &Client{channels: []*channel{{sem: make(chan struct{}, 1)}}, logger: lg}

	for range 500 {
		c.logContention()
	}

	assert.Equal(t, 1, lg.warns)
}

func TestAcquireOn_LogsOnlyOnFailure(t *testing.T) {
	lg := &countingLogger{Logger: log.NewLogger()}
	ch := &channel{sem: make(chan struct{}, 1)}
	c := &Client{channels: []*channel{ch}, logger: lg}

	require.NoError(t, c.acquireOn(context.Background(), ch))
	assert.Equal(t, 0, lg.warns)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.Error(t, c.acquireOn(ctx, ch))
	assert.Equal(t, 1, lg.warns)
}
