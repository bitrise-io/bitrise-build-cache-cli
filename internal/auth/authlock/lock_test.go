//go:build unit

package authlock

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAcquire_ReleasesSoTheNextCallerCanTakeIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.lock")

	release, err := Acquire(t.Context(), path, time.Second)
	require.NoError(t, err)
	require.NoError(t, release())

	release, err = Acquire(t.Context(), path, time.Second)
	require.NoError(t, err)
	require.NoError(t, release())
}

// Release is idempotent so a caller that defers before checking err cannot
// unlock a lock the retry then reacquired.
func TestAcquire_ReleaseIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.lock")

	release, err := Acquire(t.Context(), path, time.Second)
	require.NoError(t, err)

	require.NoError(t, release())
	require.NoError(t, release(), "double-release must not error")
	require.NoError(t, release(), "third release must remain a no-op")
}

// The failure path must also return a callable release func, so a caller that
// defers before checking err cannot panic on a nil.
func TestAcquire_ErrorPathReleaseIsANoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.lock")

	holder := flock.New(path)
	held, err := holder.TryLock()
	require.NoError(t, err)
	require.True(t, held)
	defer func() { _ = holder.Unlock() }()

	release, err := Acquire(t.Context(), path, 100*time.Millisecond)
	require.Error(t, err)
	require.NotNil(t, release)
	require.NoError(t, release())
}

func TestAcquire_WedgedLockReturnsAfterWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.lock")

	holder := flock.New(path)
	held, err := holder.TryLock()
	require.NoError(t, err)
	require.True(t, held)
	defer func() { _ = holder.Unlock() }()

	const wait = 200 * time.Millisecond
	start := time.Now()
	release, err := Acquire(t.Context(), path, wait)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.NoError(t, release())
	assert.GreaterOrEqual(t, elapsed, wait, "should have waited the full budget")
	assert.Less(t, elapsed, wait*4, "should not hang past the budget")
}

// Concurrent callers within one process serialise via the flock even though
// they share a pid.
func TestAcquire_ConcurrentContentionSerialises(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.lock")

	const callers = 6
	var (
		wg      sync.WaitGroup
		inside  atomic.Int32
		maxSeen atomic.Int32
	)

	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			release, err := Acquire(context.Background(), path, 5*time.Second)
			require.NoError(t, err)
			defer func() { _ = release() }()

			n := inside.Add(1)
			for {
				m := maxSeen.Load()
				if n <= m || maxSeen.CompareAndSwap(m, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			inside.Add(-1)
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), maxSeen.Load(), "the lock must admit exactly one caller at a time")
}

// A cancelled context aborts the wait so a caller isn't held by a wedged peer
// beyond its own budget.
func TestAcquire_ContextCancelAbortsTheWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.lock")

	holder := flock.New(path)
	held, err := holder.TryLock()
	require.NoError(t, err)
	require.True(t, held)
	defer func() { _ = holder.Unlock() }()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	release, err := Acquire(ctx, path, 5*time.Second)
	require.Error(t, err)
	require.NoError(t, release())
}
