//go:build unit

package keychain

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"
)

type fakeBackend struct {
	store    map[string]string
	getErr   error
	setErr   error
	delErr   error
	notFound bool
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{store: map[string]string{}}
}

func (f *fakeBackend) key(service, account string) string {
	return service + "|" + account
}

func (f *fakeBackend) Get(service, account string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	v, ok := f.store[f.key(service, account)]
	if !ok || f.notFound {
		return "", keyring.ErrNotFound
	}

	return v, nil
}

func (f *fakeBackend) Set(service, account, secret string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.store[f.key(service, account)] = secret

	return nil
}

func (f *fakeBackend) Delete(service, account string) error {
	if f.delErr != nil {
		return f.delErr
	}
	if _, ok := f.store[f.key(service, account)]; !ok {
		return keyring.ErrNotFound
	}
	delete(f.store, f.key(service, account))

	return nil
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	k := &Keychain{Backend: newFakeBackend()}

	creds := Credentials{AuthToken: "tok-xyz", WorkspaceID: "ws-123"}
	require.NoError(t, k.Save(creds))

	got, err := k.Load()
	require.NoError(t, err)
	assert.Equal(t, creds, got)
}

func TestLoadEmptyReturnsErrNotFound(t *testing.T) {
	k := &Keychain{Backend: newFakeBackend()}

	_, err := k.Load()
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestSaveReplacesPreviousValue(t *testing.T) {
	k := &Keychain{Backend: newFakeBackend()}

	require.NoError(t, k.Save(Credentials{AuthToken: "first", WorkspaceID: "ws-1"}))
	require.NoError(t, k.Save(Credentials{AuthToken: "second", WorkspaceID: "ws-2"}))

	got, err := k.Load()
	require.NoError(t, err)
	assert.Equal(t, "second", got.AuthToken)
	assert.Equal(t, "ws-2", got.WorkspaceID)
}

func TestClearRemoves(t *testing.T) {
	k := &Keychain{Backend: newFakeBackend()}

	require.NoError(t, k.Save(Credentials{AuthToken: "tok", WorkspaceID: "ws"}))
	require.NoError(t, k.Clear())

	_, err := k.Load()
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestClearNoopOnEmpty(t *testing.T) {
	k := &Keychain{Backend: newFakeBackend()}

	assert.NoError(t, k.Clear())
}

func TestSaveIfChanged_writesOnFirstSave(t *testing.T) {
	k := &Keychain{Backend: newFakeBackend()}

	wrote, err := k.SaveIfChanged(Credentials{AuthToken: "tok", WorkspaceID: "ws"})
	require.NoError(t, err)
	assert.True(t, wrote)
}

func TestSaveIfChanged_skipsWhenIdentical(t *testing.T) {
	be := newFakeBackend()
	k := &Keychain{Backend: be}
	creds := Credentials{AuthToken: "tok", WorkspaceID: "ws"}
	require.NoError(t, k.Save(creds))

	wrote, err := k.SaveIfChanged(creds)
	require.NoError(t, err)
	assert.False(t, wrote)
}

func TestSaveIfChanged_writesWhenDifferent(t *testing.T) {
	k := &Keychain{Backend: newFakeBackend()}
	require.NoError(t, k.Save(Credentials{AuthToken: "old", WorkspaceID: "ws"}))

	wrote, err := k.SaveIfChanged(Credentials{AuthToken: "new", WorkspaceID: "ws"})
	require.NoError(t, err)
	assert.True(t, wrote)

	got, err := k.Load()
	require.NoError(t, err)
	assert.Equal(t, "new", got.AuthToken)
}

func TestLoadWrapsBackendError(t *testing.T) {
	backendErr := errors.New("dbus connection failed")
	be := newFakeBackend()
	be.getErr = backendErr

	k := &Keychain{Backend: be}
	_, err := k.Load()

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound)
	assert.Contains(t, err.Error(), "dbus connection failed")
}
