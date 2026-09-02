package oauth

import (
	"context"
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/authlock"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// A non-nil error means "proceed unserialised". The refresh lock path is
// oauth's concern; the flock discipline lives in internal/auth/authlock.
func acquireRefreshLock(ctx context.Context) (func() error, error) {
	p, err := paths.Default()
	if err != nil {
		return func() error { return nil }, fmt.Errorf("resolve refresh lock path: %w", err)
	}

	release, err := authlock.Acquire(ctx, p.AuthRefreshLockFile(), refreshLockWait)
	if err != nil {
		return release, fmt.Errorf("refresh lock: %w", err)
	}

	return release, nil
}

// A process we queued behind may have already refreshed, and spending an
// already-rotated refresh token would invalidate the login.
func reloadStored(creds auth.TokenSet, save func(auth.TokenSet) error) (auth.TokenSet, func(auth.TokenSet) error) {
	fresh, freshSrc, err := LoadWithSource()
	if err != nil || !fresh.IsOAuthManaged() {
		return creds, save
	}
	if freshSrc != nil {
		save = func(cr auth.TokenSet) error { return saveTo(freshSrc, cr) }
	}

	return fresh, save
}
