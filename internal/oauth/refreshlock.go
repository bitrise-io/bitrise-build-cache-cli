package oauth

import (
	"context"
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/filelock"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// acquireRefreshLock serialises the token refresh across CLI processes. A
// non-nil error means "proceed unserialised"; the release func is always safe
// to call.
func acquireRefreshLock(ctx context.Context) (func() error, error) {
	p, err := paths.Default()
	if err != nil {
		return func() error { return nil }, fmt.Errorf("resolve refresh lock path: %w", err)
	}

	release, err := filelock.Acquire(ctx, p.AuthRefreshLockFile(), filelock.Options{
		Wait: refreshLockWait,
		TTL:  refreshLockTTL,
	})
	if err != nil {
		return release, fmt.Errorf("acquire refresh lock: %w", err)
	}

	return release, nil
}

// reloadUnderLock re-reads the store now that the lock is held: a process we
// queued behind may have already refreshed, and spending an already-rotated
// refresh token would invalidate the login.
func reloadUnderLock(creds Credentials, save func(Credentials) error) (Credentials, func(Credentials) error) {
	fresh, freshSrc, err := LoadWithSource()
	if err != nil || !fresh.IsOAuthManaged() {
		return creds, save
	}
	if freshSrc != nil {
		save = func(cr Credentials) error { return SaveTo(freshSrc, cr) }
	}

	return fresh, save
}
