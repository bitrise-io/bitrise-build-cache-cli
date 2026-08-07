package keychain

import (
	"encoding/json"
	"errors"
	"fmt"

	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

const (
	serviceName = "bitrise-build-cache"
	accountName = "default"
)

var ErrNotFound = errors.New("no Bitrise Build Cache credentials in keychain")

type Backend interface {
	Get(service, account string) (string, error)
	Set(service, account, secret string) error
	Delete(service, account string) error
}

type defaultBackend struct{}

func (defaultBackend) Get(service, account string) (string, error) {
	return keyring.Get(service, account) //nolint:wrapcheck // wrapped in Keychain methods
}

func (defaultBackend) Set(service, account, secret string) error {
	return keyring.Set(service, account, secret) //nolint:wrapcheck
}

func (defaultBackend) Delete(service, account string) error {
	return keyring.Delete(service, account) //nolint:wrapcheck
}

type Keychain struct {
	Backend Backend
}

func New() *Keychain {
	return &Keychain{Backend: defaultBackend{}}
}

// NewBackend returns the OS keychain backend used by Keychain — exposed for
// callers that need raw Set/Get/Delete against a non-default service/account
// (e.g. the doctor smoke-test).
func NewBackend() Backend {
	return defaultBackend{}
}

func (k *Keychain) Load() (auth.TokenSet, error) {
	raw, err := k.Backend.Get(serviceName, accountName)
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return auth.TokenSet{}, ErrNotFound
	case err != nil:
		return auth.TokenSet{}, fmt.Errorf("keychain read: %w", classify(err))
	}

	var c auth.TokenSet
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return auth.TokenSet{}, fmt.Errorf("keychain decode: %w", err)
	}

	return c, nil
}

func (k *Keychain) Save(c auth.TokenSet) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("keychain encode: %w", err)
	}

	if err := k.Backend.Set(serviceName, accountName, string(raw)); err != nil {
		return fmt.Errorf("keychain write: %w", classify(err))
	}

	return nil
}

func (k *Keychain) Clear() error {
	switch err := k.Backend.Delete(serviceName, accountName); {
	case err == nil, errors.Is(err, keyring.ErrNotFound):
		return nil
	default:
		return fmt.Errorf("keychain delete: %w", classify(err))
	}
}
