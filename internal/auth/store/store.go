// Package store picks the credential backend: CI→file (fastlane setup_ci swaps the keychain), local→keychain.
package store

import (
	"errors"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Sentinel independent of backend; callers can errors.Is either this or keychain.ErrNotFound.
var ErrNotFound = errors.New("no Bitrise Build Cache credentials in store")

// Load returns ErrNotFound when nothing is stored.
type Store interface {
	Backend() auth.Backend
	Load() (auth.TokenSet, error)
	Save(creds auth.TokenSet) error
	Clear() error
}

// CI→file, because fastlane setup_ci swaps the keychain out from under us. Takes
// the answer rather than the environment: CI-ness affects only the write target.
func SelectAuto(isCI bool) Store {
	if isCI {
		return NewFile()
	}

	return NewKeychain()
}

// override: "keychain" | "file" | "" | "auto"; empty/auto delegates to SelectAuto.
func Select(isCI bool, override string) (Store, error) {
	switch override {
	case "", "auto":
		return SelectAuto(isCI), nil
	case "keychain":
		return NewKeychain(), nil
	case "file":
		return NewFile(), nil
	}

	return nil, fmt.Errorf("unknown storage backend %q (want keychain|file|auto)", override)
}

// saveExclusive saves to target, then best-effort clears every other backend to
// prevent split-brain.
func saveExclusive(target Store, creds auth.TokenSet) error {
	if err := target.Save(creds); err != nil {
		return err //nolint:wrapcheck
	}
	for _, other := range []Store{NewKeychain(), NewFile()} {
		if other.Backend() == target.Backend() {
			continue
		}
		_ = other.Clear()
	}

	return nil
}

// KeychainErr is set only when the keychain was tried and refused; a non-nil value
// is itself the fallback signal.
type SaveResult struct {
	Origin      auth.Origin
	KeychainErr error
}

func (r SaveResult) WarnFallback(logger log.Logger) {
	if r.KeychainErr != nil && logger != nil {
		logger.Warnf("Could not write to the OS keychain (%s).", r.KeychainErr)
	}
}

// SaveExclusiveWithFallback saves to target and clears every other backend,
// dropping to the file store when the keychain refuses the write: a host with no
// secret-service (headless Linux, containers) would otherwise have no way to
// store credentials at all, short of knowing to pass --storage=file.
//
// Exclusive because it backs a deliberate user action (`auth login`, `auth set`)
// where two populated backends would be split-brain. Activation must use
// SaveWithFallback instead — see the note there.
//
// allowFallback is false when the caller picked the backend explicitly — they
// asked for that one, so silently using another would be wrong.
func SaveExclusiveWithFallback(target Store, creds auth.TokenSet, allowFallback bool) (SaveResult, error) {
	return saveExclusiveWithFallback(target, creds, allowFallback)
}

// SaveWithFallback saves to target and leaves every other backend alone. Used
// when materialising an injected credential during activation: clearing the other
// backend there would throw away a login the user deliberately stored, which is
// not something an incidental write should ever do.
//
// merge rebuilds the record against whichever backend is actually written. A
// record derived from an unreadable keychain carries no refresh token, and
// writing that to the file store is what turns a keychain outage into a lost
// login.
func SaveWithFallback(target Store, merge func(Store) auth.TokenSet, allowFallback bool) (SaveResult, error) {
	return saveWithFallback(target, merge, allowFallback, func(s Store, c auth.TokenSet) error {
		return s.Save(c) //nolint:wrapcheck // backends already wrap
	})
}

func saveExclusiveWithFallback(target Store, creds auth.TokenSet, allowFallback bool) (SaveResult, error) {
	return saveWithFallback(target, func(Store) auth.TokenSet { return creds }, allowFallback, saveExclusive)
}

func saveWithFallback(target Store, merge func(Store) auth.TokenSet, allowFallback bool, save func(Store, auth.TokenSet) error) (SaveResult, error) {
	creds := merge(target)

	err := save(target, creds)
	if err == nil {
		return SaveResult{Origin: creds.Origin(target.Backend())}, nil
	}

	if target.Backend() != auth.BackendKeychain || !allowFallback {
		return SaveResult{Origin: creds.Origin(target.Backend())}, err
	}

	fallback := NewFile()
	fbCreds := merge(fallback)
	if fbErr := save(fallback, fbCreds); fbErr != nil {
		return SaveResult{Origin: fbCreds.Origin(fallback.Backend())}, fmt.Errorf("save to the keychain (%w) and to the config file (%w)", err, fbErr)
	}

	return SaveResult{Origin: fbCreds.Origin(fallback.Backend()), KeychainErr: err}, nil
}

func NewKeychain() Store {
	return keychainStore{kc: keychain.New()}
}

func NewFile() Store {
	return fileStore{
		osProxy:        utils.DefaultOsProxy{},
		encoderFactory: utils.DefaultEncoderFactory{},
		decoderFactory: utils.DefaultDecoderFactory{},
	}
}

type keychainStore struct {
	kc *keychain.Keychain
}

func (s keychainStore) Backend() auth.Backend { return auth.BackendKeychain }

// A machine with no keychain reads as empty rather than failing, so every
// credential lookup falls through to the next backend. The keychain sentinel is
// wrapped rather than replaced, so a caller that needs to tell "nothing stored"
// from "no keyring on this host" can still errors.Is for either.
func (s keychainStore) Load() (auth.TokenSet, error) {
	creds, err := s.kc.Load()
	if errors.Is(err, keychain.ErrNotFound) || errors.Is(err, keychain.ErrUnavailable) {
		return auth.TokenSet{}, fmt.Errorf("%w: %w", ErrNotFound, err)
	}

	return creds, err //nolint:wrapcheck // keychain.Keychain already wraps
}

func (s keychainStore) Save(c auth.TokenSet) error {
	return s.kc.Save(stampSchema(c)) //nolint:wrapcheck
}

func (s keychainStore) Clear() error {
	return s.kc.Clear() //nolint:wrapcheck
}

type fileStore struct {
	osProxy        utils.OsProxy
	encoderFactory utils.EncoderFactory
	decoderFactory utils.DecoderFactory
}

func (s fileStore) Backend() auth.Backend { return auth.BackendFile }

func (s fileStore) Load() (auth.TokenSet, error) {
	creds, ok := multiplatformconfig.ReadCredentials(s.osProxy, s.decoderFactory)
	if !ok {
		return auth.TokenSet{}, ErrNotFound
	}

	return creds, nil
}

func (s fileStore) Save(c auth.TokenSet) error {
	if err := multiplatformconfig.SaveCredentials(s.osProxy, s.encoderFactory, s.decoderFactory, stampSchema(c)); err != nil {
		return fmt.Errorf("save credentials to multiplatform config: %w", err)
	}

	return nil
}

// Older readers ignore the new keys; the shape stays legible both ways.
func stampSchema(c auth.TokenSet) auth.TokenSet {
	c.SchemaVersion = auth.SchemaVersionCurrent

	return c
}

func (s fileStore) Clear() error {
	if err := multiplatformconfig.ClearCredentials(s.osProxy, s.encoderFactory, s.decoderFactory); err != nil {
		return fmt.Errorf("clear credentials from multiplatform config: %w", err)
	}

	return nil
}
