package oauth

import (
	"errors"
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

func Load() (auth.TokenSet, error) {
	c, _, err := LoadWithSource()

	return c, err
}

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

func Save(c auth.TokenSet) error {
	return SaveTo(store.NewKeychain(), c)
}

func SaveTo(s store.Store, c auth.TokenSet) error {
	_, err := SaveToWithFallback(s, c, false)

	return err
}

// SaveToWithFallback drops to the config file when the keychain refuses the
// write — a completed sign-in shouldn't be thrown away because the machine has no
// keychain. The whole credential goes to the fallback, refresh token included, so
// the login stays refreshable there.
func SaveToWithFallback(s store.Store, c auth.TokenSet, allowFallback bool) (store.SaveOutcome, error) {
	if c.AuthToken == "" {
		return store.SaveOutcome{Kind: s.Kind()}, errors.New("refusing to save credentials with empty PAT")
	}

	outcome, err := store.SaveExclusiveWithFallback(s, c, allowFallback)
	if err != nil {
		return outcome, fmt.Errorf("save credentials: %w", err)
	}

	return outcome, nil
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
