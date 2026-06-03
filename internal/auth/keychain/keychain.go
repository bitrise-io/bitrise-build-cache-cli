// Package keychain stores the Bitrise Build Cache auth credentials in the OS
// secret store (macOS Keychain, Linux secret-service) instead of forcing the
// user to keep BITRISE_BUILD_CACHE_AUTH_TOKEN in shell rc files.
//
// Credentials are JSON-encoded into a single keychain item under
// service="bitrise-build-cache", account="default".
package keychain

import (
	"encoding/json"
	"errors"
	"fmt"

	keyring "github.com/zalando/go-keyring"
)

const (
	serviceName = "bitrise-build-cache"
	accountName = "default"
)

// ErrNotFound is returned by Load when no credentials are stored yet.
var ErrNotFound = errors.New("no Bitrise Build Cache credentials in keychain")

// Credentials are the auth fields stored in the keychain.
type Credentials struct {
	AuthToken   string `json:"auth_token"`
	WorkspaceID string `json:"workspace_id"`
}

// Backend is the slice of zalando/go-keyring the Keychain depends on. Exported
// so tests can inject an in-memory implementation.
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

// Keychain reads + writes Credentials via a Backend.
type Keychain struct {
	Backend Backend
}

// New returns a Keychain backed by the real OS secret store.
func New() *Keychain {
	return &Keychain{Backend: defaultBackend{}}
}

// Load returns the stored credentials, or ErrNotFound if nothing was stored.
func (k *Keychain) Load() (Credentials, error) {
	raw, err := k.Backend.Get(serviceName, accountName)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return Credentials{}, ErrNotFound
		}

		return Credentials{}, fmt.Errorf("keychain read: %w", err)
	}

	var c Credentials
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Credentials{}, fmt.Errorf("keychain decode: %w", err)
	}

	return c, nil
}

// Save persists the credentials, replacing whatever was there before.
func (k *Keychain) Save(c Credentials) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("keychain encode: %w", err)
	}

	if err := k.Backend.Set(serviceName, accountName, string(raw)); err != nil {
		return fmt.Errorf("keychain write: %w", err)
	}

	return nil
}

// Clear removes the stored credentials. No-op when nothing is stored.
func (k *Keychain) Clear() error {
	err := k.Backend.Delete(serviceName, accountName)
	if err == nil {
		return nil
	}
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}

	return fmt.Errorf("keychain delete: %w", err)
}
