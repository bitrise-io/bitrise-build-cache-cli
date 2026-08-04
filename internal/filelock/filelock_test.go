//go:build unit

package filelock

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lockPath(t *testing.T) string {
	t.Helper()

	return filepath.Join(t.TempDir(), "nested", "test.lock")
}

func TestAcquire_ReleaseLetsTheNextCallerIn(t *testing.T) {
	path := lockPath(t)

	release, err := Acquire(t.Context(), path, time.Second, time.Minute)
	require.NoError(t, err)
	release()

	release2, err := Acquire(t.Context(), path, time.Second, time.Minute)
	require.NoError(t, err)
	release2()
}

func TestAcquire_SecondCallerWaitsThenFails(t *testing.T) {
	path := lockPath(t)

	release, err := Acquire(t.Context(), path, time.Second, time.Minute)
	require.NoError(t, err)
	defer release()

	start := time.Now()
	_, err = Acquire(t.Context(), path, 200*time.Millisecond, time.Minute)

	require.Error(t, err, "a held lock must not be handed out twice")
	assert.GreaterOrEqual(t, time.Since(start), 200*time.Millisecond, "it should wait out the budget before giving up")
}

func TestAcquire_SecondCallerGetsItAfterRelease(t *testing.T) {
	path := lockPath(t)

	release, err := Acquire(t.Context(), path, time.Second, time.Minute)
	require.NoError(t, err)

	go func() {
		time.Sleep(100 * time.Millisecond)
		release()
	}()

	release2, err := Acquire(t.Context(), path, 3*time.Second, time.Minute)
	require.NoError(t, err)
	release2()
}

// A process that died holding the lock must not wedge every later one.
func TestAcquire_BreaksStaleLock(t *testing.T) {
	path := lockPath(t)

	release, err := Acquire(t.Context(), path, time.Second, time.Minute)
	require.NoError(t, err)
	defer release()

	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))

	release2, err := Acquire(t.Context(), path, time.Second, time.Minute)
	require.NoError(t, err, "a lock older than the TTL should be broken open")
	release2()
}

func TestAcquire_CancelledCtxReturnsPromptly(t *testing.T) {
	path := lockPath(t)

	release, err := Acquire(t.Context(), path, time.Minute, time.Minute)
	require.NoError(t, err)
	defer release()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	start := time.Now()
	_, err = Acquire(ctx, path, time.Minute, time.Minute)

	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second, "a cancelled ctx must not wait out the full budget")
}

func TestClaimCooldown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marker")

	assert.True(t, ClaimCooldown(path, time.Hour), "the first claim always succeeds")
	assert.False(t, ClaimCooldown(path, time.Hour), "a claim inside the cooldown is refused")
	assert.True(t, ClaimCooldown(path, time.Nanosecond), "an elapsed cooldown claims again")
}
