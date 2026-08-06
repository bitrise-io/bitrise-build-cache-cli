package oauth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

const refreshLockPoll = 50 * time.Millisecond

// A non-nil error means "proceed unserialised".
//
// The kernel owns the lock, so a holder that is killed releases it immediately and
// cannot wedge the next refresh. The lock file is deliberately never removed:
// unlinking it would let a process holding the old inode and a process locking a
// newly created one both believe they own it.
func acquireRefreshLock(ctx context.Context) (func() error, error) {
	noop := func() error { return nil }

	p, err := paths.Default()
	if err != nil {
		return noop, fmt.Errorf("resolve refresh lock path: %w", err)
	}
	path := p.AuthRefreshLockFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return noop, fmt.Errorf("create refresh lock dir: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, refreshLockWait)
	defer cancel()

	lock := flock.New(path)
	locked, err := lock.TryLockContext(waitCtx, refreshLockPoll)
	if err != nil {
		return noop, fmt.Errorf("acquire refresh lock %s: %w", path, err)
	}
	if !locked {
		return noop, fmt.Errorf("refresh lock %s still held after %s", path, refreshLockWait)
	}

	return lock.Unlock, nil
}

// A process we queued behind may have already refreshed, and spending an
// already-rotated refresh token would invalidate the login.
func reloadStored(creds Credentials, save func(Credentials) error) (Credentials, func(Credentials) error) {
	fresh, freshSrc, err := LoadWithSource()
	if err != nil || !fresh.IsOAuthManaged() {
		return creds, save
	}
	if freshSrc != nil {
		save = func(cr Credentials) error { return SaveTo(freshSrc, cr) }
	}

	return fresh, save
}
