package oauth

import (
	"context"
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/filelock"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// A non-nil error means "proceed unserialised".
func acquireRefreshLock(ctx context.Context) (func() error, error) {
	p, err := paths.Default()
	if err != nil {
		return func() error { return nil }, fmt.Errorf("resolve refresh lock path: %w", err)
	}

	release, err := filelock.Acquire(ctx, p.AuthRefreshLockFile(), filelock.Options{
		Wait:    refreshLockWait,
		TTL:     refreshLockTTL,
		MaxHold: refreshLockMaxHold,
	})
	if err != nil {
		return release, fmt.Errorf("acquire refresh lock: %w", err)
	}

	return release, nil
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
