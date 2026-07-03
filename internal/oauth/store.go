package oauth

import (
	"errors"
	"fmt"
	"time"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
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

// Load reads the stored credential from the keychain. A missing item returns the
// zero Credentials so a not-logged-in user doesn't see an error.
func Load() (Credentials, error) {
	kc, err := keychain.New().Load()
	if errors.Is(err, keychain.ErrNotFound) {
		return Credentials{}, nil
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("load credentials: %w", err)
	}

	return fromKeychain(kc), nil
}

// Save writes c to the keychain.
func Save(c Credentials) error {
	if c.PAT == "" {
		return errors.New("refusing to save credentials with empty PAT")
	}
	if err := keychain.New().Save(c.toKeychain()); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}

	return nil
}

// Clear removes the stored credential from the keychain.
func Clear() error {
	if err := keychain.New().Clear(); err != nil {
		return fmt.Errorf("clear credentials: %w", err)
	}

	return nil
}
