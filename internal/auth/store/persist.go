package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/authlock"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// SetUsername writes name into the store that already holds credentials so a
// username-only edit can't strand an empty-token entry in the wrong backend.
// Empty name clears the override. Returns the store written to.
func SetUsername(isCI bool, name string) (auth.Origin, error) {
	target, existing := storeHoldingCreds(isCI)
	existing.Username = strings.TrimSpace(name)
	if err := target.Save(existing); err != nil {
		return existing.Origin(target.Backend()), fmt.Errorf("save display name to %s: %w", target.Backend().String(), err)
	}

	return existing.Origin(target.Backend()), nil
}

// SetWorkspaceID writes slug into the store that already holds credentials,
// leaving the token and the OAuth refresh machinery intact so completing a
// `auth login --no-workspace` costs no second browser round-trip.
func SetWorkspaceID(isCI bool, slug string) (auth.Origin, error) {
	target, existing := storeHoldingCreds(isCI)
	if strings.TrimSpace(existing.AuthToken) == "" {
		return auth.Origin{}, fmt.Errorf("no stored credentials to attach the workspace to")
	}
	existing.WorkspaceID = strings.TrimSpace(slug)
	if err := target.Save(existing); err != nil {
		return existing.Origin(target.Backend()), fmt.Errorf("save workspace to %s: %w", target.Backend().String(), err)
	}

	return existing.Origin(target.Backend()), nil
}

// Empty slug is an error. Routed through SaveWithFallback so a keychain refusal
// on headless Linux falls through to the file store rather than dropping the
// entry on the floor.
func SaveWorkspaceToken(target Store, slug string, ws auth.TokenSet) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return errors.New("workspace slug is required")
	}

	release, err := acquireWorkspaceLock(context.Background())
	if err != nil {
		return err
	}
	defer func() { _ = release() }()

	merge := func(s Store) auth.TokenSet {
		existing, loadErr := s.Load()
		if loadErr != nil {
			existing = auth.TokenSet{}
		}
		if existing.Workspaces == nil {
			existing.Workspaces = map[string]auth.TokenSet{}
		}
		existing.Workspaces[slug] = ws

		return existing
	}

	if _, err := SaveWithFallback(target, merge, true); err != nil {
		return fmt.Errorf("save workspace token: %w", err)
	}

	return nil
}

// Wedged lock returns error rather than hanging. Overridden in tests.
//
//nolint:gochecknoglobals // matches oauth.refreshLockWait
var workspaceLockWait = 4 * time.Second

func acquireWorkspaceLock(ctx context.Context) (func() error, error) {
	p, err := paths.Default()
	if err != nil {
		return func() error { return nil }, fmt.Errorf("resolve workspace lock path: %w", err)
	}

	release, err := authlock.Acquire(ctx, p.AuthWorkspaceLockFile(), workspaceLockWait)
	if err != nil {
		return release, fmt.Errorf("workspace lock: %w", err)
	}

	return release, nil
}

// StoredUsername returns the display name from whichever backend holds one. The
// name is machine-level config set by `auth username`, independent of which
// backend the credential currently lives in, so a sign-in has to carry it across
// rather than let an exclusive write drop it.
func StoredUsername() string {
	for _, s := range []Store{NewKeychain(), NewFile()} {
		if creds, err := s.Load(); err == nil {
			if v := strings.TrimSpace(creds.Username); v != "" {
				return v
			}
		}
	}

	return ""
}

func storeHoldingCreds(isCI bool) (Store, auth.TokenSet) {
	for _, s := range []Store{NewKeychain(), NewFile()} {
		creds, err := s.Load()
		if err == nil && (strings.TrimSpace(creds.AuthToken) != "" || strings.TrimSpace(creds.WorkspaceID) != "") {
			return s, creds
		}
	}

	target := SelectAuto(isCI)
	creds, _ := target.Load()

	return target, creds
}
