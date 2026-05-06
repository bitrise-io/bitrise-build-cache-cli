//go:build unit

package health_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/health"
)

func TestTracker_NoRecord(t *testing.T) {
	tracker := health.NewTracker(t.TempDir())

	ts, ok, err := tracker.LastSuccess()
	require.NoError(t, err)
	assert.False(t, ok)
	assert.True(t, ts.IsZero())
}

func TestTracker_RecordAndRead(t *testing.T) {
	before := time.Now().UTC().Truncate(time.Second)
	tracker := health.NewTracker(t.TempDir())

	tracker.RecordSuccess()

	ts, ok, err := tracker.LastSuccess()
	require.NoError(t, err)
	assert.True(t, ok)
	assert.False(t, ts.IsZero())
	assert.True(t, !ts.Before(before), "recorded time should not be before test start")
}

func TestTracker_RecordUpdatesTimestamp(t *testing.T) {
	tracker := health.NewTracker(t.TempDir())

	tracker.RecordSuccess()
	first, _, _ := tracker.LastSuccess()

	time.Sleep(2 * time.Millisecond)
	tracker.RecordSuccess()
	second, _, _ := tracker.LastSuccess()

	assert.True(t, !second.Before(first), "second record should not be earlier than first")
}

func TestTracker_ConcurrentWrites(t *testing.T) {
	tracker := health.NewTracker(t.TempDir())

	done := make(chan struct{})
	for range 20 {
		go func() {
			tracker.RecordSuccess()
			done <- struct{}{}
		}()
	}
	for range 20 {
		<-done
	}

	_, ok, err := tracker.LastSuccess()
	require.NoError(t, err)
	assert.True(t, ok)
}
