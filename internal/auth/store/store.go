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

// CI→file, local→keychain. Total function; no error path. Takes the answer, not
// the environment: CI-ness affects only the write target, and the caller already
// knows it.
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

// SaveResult reports where a save ended up. KeychainErr is set only when the
// keychain was tried and refused; a non-nil value is itself the fallback signal.
type SaveResult struct {
	Origin      auth.Origin
	KeychainErr error
}

// FellBack reports whether the save landed somewhere other than the target.
func (r SaveResult) FellBack() bool { return r.KeychainErr != nil }

// WarnFallback logs the one warning a keychain fallback warrants. No-op otherwise.
func (r SaveResult) WarnFallback(logger log.Logger) {
	if r.KeychainErr != nil && logger != nil {
		logger.Warnf("Could not write to the OS keychain (%s).", r.KeychainErr)
	}
}

// SaveWithFallback saves to target, dropping to the file store when the
// keychain refuses the write: a host with no secret-service (headless Linux,
// containers) would otherwise have no way to store credentials at all, short of
// knowing to pass --storage=file.
//
// allowFallback is false when the caller picked the backend explicitly — they
// asked for that one, so silently using another would be wrong.
func SaveWithFallback(target Store, creds auth.TokenSet, allowFallback bool) (SaveResult, error) {
	err := saveExclusive(target, creds)
	if err == nil {
		return SaveResult{Origin: creds.Origin(target.Backend())}, nil
	}

	if target.Backend() != auth.BackendKeychain || !allowFallback {
		return SaveResult{Origin: creds.Origin(target.Backend())}, err
	}

	fallback := NewFile()
	if fbErr := saveExclusive(fallback, creds); fbErr != nil {
		return SaveResult{Origin: creds.Origin(fallback.Backend())}, fmt.Errorf("save to the keychain (%w) and to the config file (%w)", err, fbErr)
	}

	return SaveResult{Origin: creds.Origin(fallback.Backend()), KeychainErr: err}, nil
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
// credential lookup falls through to the next backend. `auth status` and the
// doctor call the keychain directly and keep the distinction.
func (s keychainStore) Load() (auth.TokenSet, error) {
	creds, err := s.kc.Load()
	if errors.Is(err, keychain.ErrNotFound) || errors.Is(err, keychain.ErrUnavailable) {
		return auth.TokenSet{}, ErrNotFound
	}

	return creds, err //nolint:wrapcheck // keychain.Keychain already wraps
}

func (s keychainStore) Save(c auth.TokenSet) error {
	return s.kc.Save(c) //nolint:wrapcheck
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
	if err := multiplatformconfig.SaveCredentials(s.osProxy, s.encoderFactory, s.decoderFactory, c); err != nil {
		return fmt.Errorf("save credentials to multiplatform config: %w", err)
	}

	return nil
}

func (s fileStore) Clear() error {
	if err := multiplatformconfig.ClearCredentials(s.osProxy, s.encoderFactory, s.decoderFactory); err != nil {
		return fmt.Errorf("clear credentials from multiplatform config: %w", err)
	}

	return nil
}
