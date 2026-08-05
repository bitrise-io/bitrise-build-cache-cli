//go:build unit

package filelock

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func lockPath(t *testing.T) string {
	t.Helper()

	return filepath.Join(t.TempDir(), "nested", "test.lock")
}

func blocking() Options {
	return Options{Wait: time.Second, TTL: time.Minute}
}

func dead() AliveFn { return func(int) bool { return false } }

func alive() AliveFn { return func(int) bool { return true } }

func TestAcquire_ReleaseLetsTheNextCallerIn(t *testing.T) {
	path := lockPath(t)

	release, err := Acquire(t.Context(), path, blocking())
	require.NoError(t, err)
	require.NoError(t, release())

	release2, err := Acquire(t.Context(), path, blocking())
	require.NoError(t, err)
	require.NoError(t, release2())
}

func TestAcquire_SecondCallerWaitsThenFails(t *testing.T) {
	path := lockPath(t)

	release, err := Acquire(t.Context(), path, blocking())
	require.NoError(t, err)
	defer func() { _ = release() }()

	start := time.Now()
	opts := blocking()
	opts.Wait = 200 * time.Millisecond
	_, err = Acquire(t.Context(), path, opts)

	require.ErrorIs(t, err, ErrHeld, "a held lock must not be handed out twice")
	assert.GreaterOrEqual(t, time.Since(start), 200*time.Millisecond, "it should wait out the budget before giving up")
}

func TestAcquire_SecondCallerGetsItAfterRelease(t *testing.T) {
	path := lockPath(t)

	release, err := Acquire(t.Context(), path, blocking())
	require.NoError(t, err)

	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = release()
	}()

	opts := blocking()
	opts.Wait = 3 * time.Second
	release2, err := Acquire(t.Context(), path, opts)
	require.NoError(t, err)
	require.NoError(t, release2())
}

// Liveness answers this exactly, without waiting out a TTL.
func TestAcquire_DeadOwnerIsBrokenOpenImmediately(t *testing.T) {
	path := lockPath(t)

	release, err := Acquire(t.Context(), path, blocking())
	require.NoError(t, err)
	defer func() { _ = release() }()

	opts := Options{Wait: 0, TTL: time.Hour, IsAlive: dead()}
	start := time.Now()
	release2, err := Acquire(t.Context(), path, opts)

	require.NoError(t, err, "a marker whose owner is gone should be broken open")
	assert.Less(t, time.Since(start), time.Second, "liveness should not wait for the TTL to elapse")
	require.NoError(t, release2())
}

// A slow-but-alive holder used to be broken open after the TTL, which let two
// processes spend the same rotated refresh token.
func TestAcquire_LiveOwnerIsNeverBrokenOpen(t *testing.T) {
	path := lockPath(t)

	release, err := Acquire(t.Context(), path, blocking())
	require.NoError(t, err)
	defer func() { _ = release() }()

	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))

	_, err = Acquire(t.Context(), path, Options{Wait: 0, TTL: time.Nanosecond, IsAlive: alive()})
	require.ErrorIs(t, err, ErrHeld, "an owner that is still running keeps the lock however old the marker is")
}

// A writer that died mid-claim leaves no pid to probe.
func TestAcquire_MarkerWithoutPidFallsBackToTTL(t *testing.T) {
	path := lockPath(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("   "), 0o600))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))

	release, err := Acquire(t.Context(), path, Options{Wait: 0, TTL: time.Minute})
	require.NoError(t, err, "an unattributable marker older than the TTL should be broken open")
	require.NoError(t, release())
}

func TestAcquire_MarkerWithoutPidHeldWithinTTL(t *testing.T) {
	path := lockPath(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("not-a-pid"), 0o600))

	_, err := Acquire(t.Context(), path, Options{Wait: 0, TTL: time.Hour})
	require.ErrorIs(t, err, ErrHeld)
}

// What the proxy startup wants: contention means someone else is doing the job.
func TestAcquire_ZeroWaitFailsImmediately(t *testing.T) {
	path := lockPath(t)

	release, err := Acquire(t.Context(), path, Options{})
	require.NoError(t, err)
	defer func() { _ = release() }()

	start := time.Now()
	_, err = Acquire(t.Context(), path, Options{IsAlive: alive()})

	require.ErrorIs(t, err, ErrHeld)
	assert.Less(t, time.Since(start), 500*time.Millisecond, "no Wait means no polling")
}

func TestAcquire_ReclaimAllowsSelfOwnedMarker(t *testing.T) {
	path := lockPath(t)

	release, err := Acquire(t.Context(), path, Options{})
	require.NoError(t, err)
	defer func() { _ = release() }()

	release2, err := Acquire(t.Context(), path, Options{Reclaim: true, IsAlive: alive()})
	require.NoError(t, err, "a marker this process already owns is not a foreign owner")
	require.NoError(t, release2())

	_, err = Acquire(t.Context(), path, Options{IsAlive: alive()})
	require.NoError(t, err, "without Reclaim a self-owned live marker still blocks")
}

func TestAcquire_ReleaseIsIdempotent(t *testing.T) {
	path := lockPath(t)

	release, err := Acquire(t.Context(), path, Options{})
	require.NoError(t, err)

	require.NoError(t, release())
	require.NoError(t, release(), "releasing an already-removed marker is not an error")
}

func TestAcquire_CancelledCtxReturnsPromptly(t *testing.T) {
	path := lockPath(t)

	opts := blocking()
	opts.Wait = time.Minute
	release, err := Acquire(t.Context(), path, opts)
	require.NoError(t, err)
	defer func() { _ = release() }()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	start := time.Now()
	_, err = Acquire(ctx, path, opts)

	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second, "a cancelled ctx must not wait out the full budget")
}

func TestReadOwner(t *testing.T) {
	osProxy := utils.DefaultOsProxy{}

	t.Run("missing marker", func(t *testing.T) {
		pid, alive := ReadOwner(osProxy, filepath.Join(t.TempDir(), "absent"))
		assert.Zero(t, pid)
		assert.False(t, alive)
	})

	t.Run("live owner", func(t *testing.T) {
		path := lockPath(t)
		release, err := Acquire(t.Context(), path, Options{})
		require.NoError(t, err)
		defer func() { _ = release() }()

		pid, alive := ReadOwner(osProxy, path)
		assert.Equal(t, os.Getpid(), pid, "the claim records the owning pid")
		assert.True(t, alive)
	})

	t.Run("dead owner", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "marker")
		require.NoError(t, os.WriteFile(path, []byte(strconv.Itoa(math.MaxInt32)), 0o600))

		pid, alive := ReadOwner(osProxy, path)
		assert.Equal(t, math.MaxInt32, pid)
		assert.False(t, alive, "a pid nothing is running under reads as dead")
	})

	t.Run("malformed marker", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "marker")
		require.NoError(t, os.WriteFile(path, []byte("garbage"), 0o600))

		pid, alive := ReadOwner(osProxy, path)
		assert.Zero(t, pid)
		assert.False(t, alive)
	})
}

func TestClaimCooldown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marker")

	assert.True(t, ClaimCooldown(path, time.Hour), "the first claim always succeeds")
	assert.False(t, ClaimCooldown(path, time.Hour), "a claim inside the cooldown is refused")
	assert.True(t, ClaimCooldown(path, time.Nanosecond), "an elapsed cooldown claims again")
}
