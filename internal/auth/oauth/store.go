package oauth

import (
	"errors"
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

// Second return is nil when nothing was found; refresh flows save back into the same store.
func LoadWithSource() (auth.TokenSet, store.Store, error) {
	return loadFrom(store.NewKeychain(), store.NewFile())
}

// loadFrom prefers an OAuth-managed credential over a manual one wherever it
// lives: a plain `auth set` PAT in an earlier backend would otherwise hide a
// login stored in a later one, so logout and refresh would both miss it.
func loadFrom(backends ...store.Store) (auth.TokenSet, store.Store, error) {
	var (
		firstCreds auth.TokenSet
		firstStore store.Store
	)

	for _, s := range backends {
		kc, err := s.Load()
		switch {
		case errors.Is(err, store.ErrNotFound):
			continue
		case err != nil:
			return auth.TokenSet{}, nil, fmt.Errorf("load credentials: %w", err)
		}

		creds := kc
		if creds.IsOAuthManaged() {
			return creds, s, nil
		}
		if firstStore == nil {
			firstCreds, firstStore = creds, s
		}
	}

	if firstStore != nil {
		return firstCreds, firstStore, nil
	}

	return auth.TokenSet{}, nil, nil
}

// No fallback: the refresh flow writes back to the store the record came from, and
// moving it mid-refresh would leave two backends disagreeing.
func saveTo(s store.Store, c auth.TokenSet) error {
	_, err := SaveToWithFallback(s, c, false)

	return err
}

// SaveToWithFallback persists a completed sign-in, dropping to the config file
// when the keychain refuses the write — a finished sign-in shouldn't be thrown
// away because the machine has no keychain. The whole record goes to the
// fallback, refresh token included, so the login stays refreshable there.
func SaveToWithFallback(s store.Store, c auth.TokenSet, allowFallback bool) (store.SaveResult, error) {
	if c.AuthToken == "" {
		return store.SaveResult{Origin: c.Origin(s.Backend())}, errors.New("refusing to save credentials with empty PAT")
	}

	result, err := store.SaveExclusiveWithFallback(s, c, allowFallback)
	if err != nil {
		return result, fmt.Errorf("save credentials: %w", err)
	}

	return result, nil
}

func ClearFrom(backends ...store.Store) error {
	for _, s := range backends {
		if err := s.Clear(); err != nil {
			return fmt.Errorf("clear credentials: %w", err)
		}
	}

	return nil
}
