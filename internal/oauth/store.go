package oauth

import (
	"errors"
	"fmt"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

// Credentials is the OAuth login credential, persisted in the OS keychain as a
// keychain.Credentials. WorkspaceID is stored because the cache is
// workspace-scoped while the OAuth login is user-scoped.
type Credentials struct {
	PAT                string
	PATExpiry          time.Time
	JWT                string
	JWTExpiry          time.Time
	RefreshToken       string
	RefreshTokenExpiry time.Time
	WorkspaceID        string
}

// IsOAuthManaged reports whether the credential came from OAuth (has a refresh token).
func (c Credentials) IsOAuthManaged() bool {
	return c.RefreshToken != ""
}

func (c Credentials) toKeychain() keychain.Credentials {
	return keychain.Credentials{
		AuthToken:          c.PAT,
		WorkspaceID:        c.WorkspaceID,
		PATExpiry:          c.PATExpiry,
		JWT:                c.JWT,
		JWTExpiry:          c.JWTExpiry,
		RefreshToken:       c.RefreshToken,
		RefreshTokenExpiry: c.RefreshTokenExpiry,
	}
}

func fromKeychain(kc keychain.Credentials) Credentials {
	return Credentials{
		PAT:                kc.AuthToken,
		PATExpiry:          kc.PATExpiry,
		JWT:                kc.JWT,
		JWTExpiry:          kc.JWTExpiry,
		RefreshToken:       kc.RefreshToken,
		RefreshTokenExpiry: kc.RefreshTokenExpiry,
		WorkspaceID:        kc.WorkspaceID,
	}
}

func Load() (Credentials, error) {
	c, _, err := LoadWithSource()

	return c, err
}

// Second return is nil when nothing was found; refresh flows save back into the same store.
func LoadWithSource() (Credentials, store.Store, error) {
	return loadFrom(store.NewKeychain(), store.NewFile())
}

func loadFrom(backends ...store.Store) (Credentials, store.Store, error) {
	for _, s := range backends {
		kc, err := s.Load()
		switch {
		case errors.Is(err, store.ErrNotFound):
			continue
		case err != nil:
			return Credentials{}, nil, fmt.Errorf("load credentials: %w", err)
		}

		return fromKeychain(kc), s, nil
	}

	return Credentials{}, nil, nil
}

func Save(c Credentials) error {
	return SaveTo(store.NewKeychain(), c)
}

func SaveTo(s store.Store, c Credentials) error {
	if c.PAT == "" {
		return errors.New("refusing to save credentials with empty PAT")
	}
	if err := store.SaveExclusive(s, c.toKeychain()); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}

	return nil
}

func Clear() error {
	return ClearFrom(store.NewKeychain(), store.NewFile())
}

func ClearFrom(backends ...store.Store) error {
	for _, s := range backends {
		if err := s.Clear(); err != nil {
			return fmt.Errorf("clear credentials: %w", err)
		}
	}

	return nil
}
