//go:build unit

package keychain

import (
	"errors"
	"fmt"
	"testing"

	dbus "github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/assert"
	keyring "github.com/zalando/go-keyring"
)

// The exact error a Linux host with no secret-service returns, captured from a
// headless Ubuntu VM. Matched on the dbus error name rather than the message,
// which is human-facing text.
func serviceUnknownErr() error {
	return dbus.Error{
		Name: "org.freedesktop.DBus.Error.ServiceUnknown",
		Body: []interface{}{"The name org.freedesktop.secrets was not provided by any .service files"},
	}
}

func TestUnavailable(t *testing.T) {
	assert.False(t, Unavailable(nil))
	assert.False(t, Unavailable(errors.New("boom")))
	assert.False(t, Unavailable(keyring.ErrNotFound))
	assert.False(t, Unavailable(dbus.Error{Name: "org.freedesktop.Secret.Error.IsLocked"}),
		"a locked keychain exists — it just refused this call")

	assert.True(t, Unavailable(serviceUnknownErr()))
	assert.True(t, Unavailable(fmt.Errorf("keychain read: %w", serviceUnknownErr())))
	assert.True(t, Unavailable(keyring.ErrUnsupportedPlatform))
	assert.True(t, Unavailable(dbus.Error{Name: "org.freedesktop.DBus.Error.Spawn.ExecFailed"}))
	assert.True(t, Unavailable(errors.New("dbus: couldn't determine address of session bus")))
}

func TestKeychainOperationsReportUnavailable(t *testing.T) {
	kc := &Keychain{Backend: unavailableBackend{}}

	_, loadErr := kc.Load()
	saveErr := kc.Save(Credentials{AuthToken: "t", WorkspaceID: "w"})
	clearErr := kc.Clear()

	for name, err := range map[string]error{"load": loadErr, "save": saveErr, "clear": clearErr} {
		assert.ErrorIs(t, err, ErrUnavailable, name)
		assert.Contains(t, err.Error(), "org.freedesktop.secrets", name+" keeps the original cause")
	}
}

// A keychain that exists but rejects the call must not be reported as absent.
func TestKeychainOperationsKeepRealErrors(t *testing.T) {
	kc := &Keychain{Backend: lockedBackend{}}

	_, err := kc.Load()

	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrUnavailable)
}

type unavailableBackend struct{}

func (unavailableBackend) Get(string, string) (string, error) { return "", serviceUnknownErr() }
func (unavailableBackend) Set(string, string, string) error   { return serviceUnknownErr() }
func (unavailableBackend) Delete(string, string) error        { return serviceUnknownErr() }

type lockedBackend struct{}

func (lockedBackend) Get(string, string) (string, error) {
	return "", dbus.Error{Name: "org.freedesktop.Secret.Error.IsLocked"}
}
func (lockedBackend) Set(string, string, string) error { return nil }
func (lockedBackend) Delete(string, string) error      { return nil }
