//go:build unit

package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
)

func TestSelect_defaultsToKeychainLocally(t *testing.T) {
	s, err := Select(map[string]string{}, "")
	require.NoError(t, err)
	assert.Equal(t, KindKeychain, s.Kind())
}

func TestSelect_defaultsToFileOnCI(t *testing.T) {
	envs := map[string]string{"CIRCLECI": "true"}
	s, err := Select(envs, "")
	require.NoError(t, err)
	assert.Equal(t, KindFile, s.Kind())
}

func TestSelect_overrides(t *testing.T) {
	envs := map[string]string{"CIRCLECI": "true"} // CI, but override
	kc, err := Select(envs, "keychain")
	require.NoError(t, err)
	assert.Equal(t, KindKeychain, kc.Kind())

	fs, err := Select(map[string]string{}, "file")
	require.NoError(t, err)
	assert.Equal(t, KindFile, fs.Kind())
}

func TestSelect_unknownOverrideErrors(t *testing.T) {
	_, err := Select(map[string]string{}, "vault")
	require.Error(t, err)
}

func TestKeychainStore_LoadNotFoundMapsToErrNotFound(t *testing.T) {
	keyring.MockInit()
	s := NewKeychain()
	_, err := s.Load()
	require.ErrorIs(t, err, ErrNotFound)
}

func TestSaveExclusive_ClearsOtherBackend(t *testing.T) {
	keyring.MockInit()
	home := t.TempDir()
	t.Setenv("HOME", home)

	kc := NewKeychain()
	require.NoError(t, kc.Save(keychain.Credentials{AuthToken: "old", WorkspaceID: "old-ws"}))

	require.NoError(t, SaveExclusive(NewFile(), keychain.Credentials{AuthToken: "new", WorkspaceID: "new-ws"}))

	_, err := kc.Load()
	require.ErrorIs(t, err, ErrNotFound, "keychain must be cleared after exclusive file save")

	got, err := NewFile().Load()
	require.NoError(t, err)
	assert.Equal(t, "new", got.AuthToken)
}

func TestSetUsername_landsInStoreHoldingCredsAndPreservesAuth(t *testing.T) {
	keyring.MockInit()
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Creds live only in the file store; keychain is empty.
	require.NoError(t, NewFile().Save(keychain.Credentials{AuthToken: "tok", WorkspaceID: "ws"}))

	kind, err := SetUsername(map[string]string{}, "erin")
	require.NoError(t, err)
	assert.Equal(t, KindFile, kind, "username must land in the file store that holds the creds, not the keychain")

	got, err := NewFile().Load()
	require.NoError(t, err)
	assert.Equal(t, "erin", got.Username)
	assert.Equal(t, "tok", got.AuthToken, "token must survive a username-only write")
	assert.Equal(t, "ws", got.WorkspaceID)

	_, kcErr := NewKeychain().Load()
	require.ErrorIs(t, kcErr, ErrNotFound, "username write must not create a stray keychain entry")
}

func TestFileStore_SavePersistsAtRestrictedPerms(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	s := NewFile()
	require.NoError(t, s.Save(keychain.Credentials{AuthToken: "t", WorkspaceID: "w"}))

	info, err := os.Stat(filepath.Join(home, ".bitrise", "analytics", "multiplatform", "config.json"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "file must be 0600 (token + refresh token inside)")

	dirInfo, err := os.Stat(filepath.Join(home, ".bitrise", "analytics", "multiplatform"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
}

func TestFileStore_RoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".bitrise", "analytics", "multiplatform"), 0o755))

	s := NewFile()
	_, err := s.Load()
	require.ErrorIs(t, err, ErrNotFound)

	want := keychain.Credentials{AuthToken: "tok", WorkspaceID: "ws", Username: "u"}
	require.NoError(t, s.Save(want))

	got, err := s.Load()
	require.NoError(t, err)
	assert.Equal(t, want, got)

	require.NoError(t, s.Clear())
	_, err = s.Load()
	require.ErrorIs(t, err, ErrNotFound)
}
