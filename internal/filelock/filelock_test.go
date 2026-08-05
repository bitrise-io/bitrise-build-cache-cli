//go:build unit

package filelock

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
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

// Reclaim is for a marker carrying our pid that we are not actually holding —
// granting one we still hold would let the first release delete the second's.
func TestAcquire_ReclaimOnlyForAMarkerWeDoNotHold(t *testing.T) {
	path := lockPath(t)

	release, err := Acquire(t.Context(), path, Options{})
	require.NoError(t, err)

	_, err = Acquire(t.Context(), path, Options{Reclaim: true, IsAlive: alive()})
	require.ErrorIs(t, err, ErrHeld, "we are still holding it, so it is not free to reclaim")
	require.NoError(t, release())

	// Same pid in the marker, but nothing in this process holds it.
	require.NoError(t, os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600))
	release2, err := Acquire(t.Context(), path, Options{Reclaim: true, IsAlive: alive()})
	require.NoError(t, err, "an unheld marker with our own pid is reclaimable")
	require.NoError(t, release2())
}

// A recycled pid or an unreaped zombie answers signal 0, so liveness alone would
// hold the lock forever. MaxHold is the ceiling that stops that.
func TestAcquire_MaxHoldBreaksAMarkerWhoseLivePidIsStale(t *testing.T) {
	path := lockPath(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("4242"), 0o600))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))

	_, err := Acquire(t.Context(), path, Options{Wait: 0, IsAlive: alive()})
	require.ErrorIs(t, err, ErrHeld, "without MaxHold a live-looking pid holds it forever")

	release, err := Acquire(t.Context(), path, Options{Wait: 0, MaxHold: time.Minute, IsAlive: alive()})
	require.NoError(t, err, "past the ceiling the marker is broken open")
	require.NoError(t, release())
}

// Releasing by path would unlink a successor's marker and admit a third holder.
func TestRelease_WillNotRemoveAMarkerThatIsNoLongerOurs(t *testing.T) {
	path := lockPath(t)

	release, err := Acquire(t.Context(), path, Options{})
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(path, []byte("4242"), 0o600))

	require.Error(t, release(), "the marker belongs to someone else now")
	pid, _ := ReadOwner(utils.DefaultOsProxy{}, path)
	assert.Equal(t, 4242, pid, "the successor keeps its lock")
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

// openHook fires just before the steal guard is created, which is the window a
// stealer's verdict can go stale in.
type openHook struct {
	utils.DefaultOsProxy
	before func(name string)
}

func (p openHook) OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	if p.before != nil {
		p.before(name)
	}

	return p.DefaultOsProxy.OpenFile(name, flag, perm)
}

// A faster stealer can replace the corpse with its own live marker between our
// verdict and the destructive step. Removing on the stale verdict would hand the
// lock to two owners at once, which for the token refresh burns the refresh token.
func TestSteal_StaleVerdictDoesNotRemoveALiveMarker(t *testing.T) {
	path := lockPath(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(strconv.Itoa(math.MaxInt32)), 0o600))

	const liveOwner = 4242
	swapped := false
	proxy := openHook{before: func(name string) {
		if swapped || !strings.HasSuffix(name, stealGuardSuffix) {
			return
		}
		swapped = true
		require.NoError(t, os.WriteFile(path, []byte(strconv.Itoa(liveOwner)), 0o600))
	}}

	_, err := Acquire(t.Context(), path, Options{
		Wait:    0,
		Os:      proxy,
		IsAlive: func(pid int) bool { return pid == liveOwner },
	})

	require.ErrorIs(t, err, ErrHeld, "the lock belongs to whoever claimed it while we looked away")

	pid, _ := ReadOwner(utils.DefaultOsProxy{}, path)
	assert.Equal(t, liveOwner, pid, "a marker that turned live must be left alone")
}

// The guard admits one stealer at a time; the rest back off rather than pile in.
func TestSteal_GuardHeldByAnotherStealerBlocksTheBreakOpen(t *testing.T) {
	path := lockPath(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(strconv.Itoa(math.MaxInt32)), 0o600))
	require.NoError(t, os.WriteFile(path+stealGuardSuffix, []byte(""), 0o600))

	_, err := Acquire(t.Context(), path, Options{Wait: 0, IsAlive: dead()})
	require.ErrorIs(t, err, ErrHeld, "another stealer is mid-steal, so wait rather than race it")

	pid, _ := ReadOwner(utils.DefaultOsProxy{}, path)
	assert.Equal(t, math.MaxInt32, pid, "the corpse is not ours to remove while the guard is held")
}

// A stealer that died holding the guard must not wedge break-open for good.
func TestSteal_AbandonedGuardIsDropped(t *testing.T) {
	path := lockPath(t)
	guard := path + stealGuardSuffix
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(strconv.Itoa(math.MaxInt32)), 0o600))
	require.NoError(t, os.WriteFile(guard, []byte(""), 0o600))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(guard, old, old))

	release, err := Acquire(t.Context(), path, Options{Wait: time.Second, IsAlive: dead()})
	require.NoError(t, err, "an abandoned guard should be dropped and the corpse then stolen")
	require.NoError(t, release())

	_, err = os.Stat(guard)
	assert.True(t, os.IsNotExist(err), "the guard must not outlive the steal")
}

// Concurrent break-open of one corpse must still hand the lock out one at a time.
func TestSteal_ConcurrentStealersNeverOverlap(t *testing.T) {
	path := lockPath(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(strconv.Itoa(math.MaxInt32)), 0o600))

	// Only the seeded corpse is dead; a marker written by one of these goroutines
	// carries our live pid, so the others must wait rather than steal it.
	onlySelfAlive := func(pid int) bool { return pid == os.Getpid() }

	var mu sync.Mutex
	held, maxHeld, acquired := 0, 0, 0
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := Acquire(t.Context(), path, Options{Wait: 5 * time.Second, IsAlive: onlySelfAlive})
			if err != nil {
				return
			}
			mu.Lock()
			held++
			acquired++
			maxHeld = max(maxHeld, held)
			mu.Unlock()

			time.Sleep(5 * time.Millisecond)

			mu.Lock()
			held--
			mu.Unlock()
			_ = release()
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, maxHeld, "two processes must never hold the lock at once")
	assert.Equal(t, 8, acquired, "every caller should get its turn")

	leftovers, err := filepath.Glob(path + ".stale.*")
	require.NoError(t, err)
	assert.Empty(t, leftovers, "steals must not litter")
}

func TestAliveFromSignalErr(t *testing.T) {
	assert.True(t, aliveFromSignalErr(nil), "signal delivered: the process is there")
	assert.True(t, aliveFromSignalErr(syscall.EPERM), "EPERM means it exists but is another user's")
	assert.False(t, aliveFromSignalErr(syscall.ESRCH), "ESRCH is the only real 'it is gone'")
	assert.False(t, aliveFromSignalErr(os.ErrProcessDone))
}

func TestClaimCooldown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marker")

	assert.True(t, ClaimCooldown(path, time.Hour), "the first claim always succeeds")
	assert.False(t, ClaimCooldown(path, time.Hour), "a claim inside the cooldown is refused")
	assert.True(t, ClaimCooldown(path, time.Nanosecond), "an elapsed cooldown claims again")
}

// All N helpers of a build start at once and all read the same elapsed window, so
// without serialisation the "rate-limited" warning prints N times.
func TestClaimCooldown_ConcurrentClaimantsFireOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marker")
	require.True(t, ClaimCooldown(path, time.Hour))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))

	var mu sync.Mutex
	claims := 0
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ClaimCooldown(path, time.Minute) {
				mu.Lock()
				claims++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, claims, "exactly one caller may claim an elapsed window")
}
