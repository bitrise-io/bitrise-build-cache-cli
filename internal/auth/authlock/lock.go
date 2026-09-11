// Package authlock is the shared cross-process flock primitive used by every
// auth read-modify-write. The kernel owns the lock, so a holder that is killed
// releases it immediately and cannot wedge the next caller. Lock files are
// deliberately never removed: unlinking would let a process holding the old
// inode and one locking a newly created one both believe they own it.
package authlock

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

const pollInterval = 50 * time.Millisecond

// Acquire flock-serialises access to path. The returned release func must be
// called even when err is non-nil, and is idempotent.
func Acquire(ctx context.Context, path string, wait time.Duration) (func() error, error) {
	noop := func() error { return nil }

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return noop, fmt.Errorf("create lock dir: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()

	lock := flock.New(path)
	locked, err := lock.TryLockContext(waitCtx, pollInterval)
	if err != nil {
		return noop, fmt.Errorf("acquire lock %s: %w", path, err)
	}
	if !locked {
		return noop, fmt.Errorf("lock %s still held after %s", path, wait)
	}

	return releaseOnce(lock), nil
}

func releaseOnce(lock *flock.Flock) func() error {
	var (
		once sync.Once
		err  error
	)

	return func() error {
		once.Do(func() {
			if e := lock.Unlock(); e != nil {
				err = fmt.Errorf("release lock: %w", e)
			}
		})

		return err
	}
}
