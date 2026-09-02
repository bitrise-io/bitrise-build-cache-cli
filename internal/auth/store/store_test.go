//go:build unit

package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

func TestSelect_defaultsToKeychainLocally(t *testing.T) {
	s, err := Select(false, "")
	require.NoError(t, err)
	assert.Equal(t, auth.BackendKeychain, s.Backend())
}

func TestSelect_defaultsToFileOnCI(t *testing.T) {
	s, err := Select(true, "")
	require.NoError(t, err)
	assert.Equal(t, auth.BackendFile, s.Backend())
}

func TestSelect_overrides(t *testing.T) {
	kc, err := Select(true, "keychain") // CI, but override
	require.NoError(t, err)
	assert.Equal(t, auth.BackendKeychain, kc.Backend())

	fs, err := Select(false, "file")
	require.NoError(t, err)
	assert.Equal(t, auth.BackendFile, fs.Backend())
}

func TestSelect_unknownOverrideErrors(t *testing.T) {
	_, err := Select(false, "vault")
	require.Error(t, err)
}

func TestKeychainStore_LoadNotFoundMapsToErrNotFound(t *testing.T) {
	keyring.MockInit()
	s := NewKeychain()
	_, err := s.Load()
	require.ErrorIs(t, err, ErrNotFound)
}

func TestKeychainStore_RoundTripStampsSchemaCurrent(t *testing.T) {
	keyring.MockInit()

	s := NewKeychain()
	require.NoError(t, s.Save(auth.TokenSet{AuthToken: "tok", WorkspaceID: "ws"}))

	got, err := s.Load()
	require.NoError(t, err)
	assert.Equal(t, auth.SchemaVersionCurrent, got.SchemaVersion, "writer must stamp the current schema tag")
}

func TestSaveExclusive_ClearsOtherBackend(t *testing.T) {
	keyring.MockInit()
	home := t.TempDir()
	t.Setenv("HOME", home)

	kc := NewKeychain()
	require.NoError(t, kc.Save(auth.TokenSet{AuthToken: "old", WorkspaceID: "old-ws"}))

	require.NoError(t, saveExclusive(NewFile(), auth.TokenSet{AuthToken: "new", WorkspaceID: "new-ws"}))

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
	require.NoError(t, NewFile().Save(auth.TokenSet{AuthToken: "tok", WorkspaceID: "ws"}))

	origin, err := SetUsername(false, "erin")
	require.NoError(t, err)
	assert.Equal(t, auth.BackendFile, origin.Backend, "username must land in the file store that holds the creds, not the keychain")

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
	require.NoError(t, s.Save(auth.TokenSet{AuthToken: "t", WorkspaceID: "w"}))

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

	want := auth.TokenSet{AuthToken: "tok", WorkspaceID: "ws", Username: "u"}
	require.NoError(t, s.Save(want))

	got, err := s.Load()
	require.NoError(t, err)
	// Save stamps the current schema tag; ignore it from the round-trip diff.
	want.SchemaVersion = got.SchemaVersion
	assert.Equal(t, want, got)
	assert.Equal(t, auth.SchemaVersionCurrent, got.SchemaVersion, "writer must stamp the current schema tag")

	require.NoError(t, s.Clear())
	_, err = s.Load()
	require.ErrorIs(t, err, ErrNotFound)
}

// The display name is machine-level config set by `auth username`, so a sign-in
// has to carry it across even when the exclusive write lands in a different
// backend than the one holding it.
func TestStoredUsername_FoundInEitherBackend(t *testing.T) {
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())

	assert.Empty(t, StoredUsername(), "nothing stored yet")

	require.NoError(t, NewFile().Save(auth.TokenSet{AuthToken: "t", WorkspaceID: "w", Username: "from-file"}))
	assert.Equal(t, "from-file", StoredUsername(), "found even when only the file store has it")

	require.NoError(t, NewKeychain().Save(auth.TokenSet{AuthToken: "t", WorkspaceID: "w", Username: "from-keychain"}))
	assert.Equal(t, "from-keychain", StoredUsername(), "the keychain is consulted first")
}

func TestSetWorkspaceID_completesAWorkspacelessLogin(t *testing.T) {
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())

	require.NoError(t, NewFile().Save(auth.TokenSet{AuthToken: "pat", RefreshToken: "refresh", Username: "erin"}))

	origin, err := SetWorkspaceID(false, "  ws-slug  ")
	require.NoError(t, err)
	assert.Equal(t, auth.BackendFile, origin.Backend)
	assert.Equal(t, auth.ProvenanceOAuthLogin, origin.Provenance)

	got, err := NewFile().Load()
	require.NoError(t, err)
	assert.Equal(t, "ws-slug", got.WorkspaceID)
	assert.Equal(t, "refresh", got.RefreshToken, "the login must stay refreshable after picking a workspace")
	assert.Equal(t, "pat", got.AuthToken)
	assert.Equal(t, "erin", got.Username)

	_, kcErr := NewKeychain().Load()
	require.ErrorIs(t, kcErr, ErrNotFound, "workspace write must not create a stray keychain entry")
}

func TestSetWorkspaceID_withNothingStoredErrors(t *testing.T) {
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())

	_, err := SetWorkspaceID(false, "ws-slug")
	require.Error(t, err)
}

// A schema-1 blob has no `schema` key; the reader must still see it as the
// machine-wide credential, and the next Save must upgrade it to the current tag.
func TestFileStore_ReadsSchemaOneBlobAndUpgradesOnWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".bitrise", "analytics", "multiplatform")
	require.NoError(t, os.MkdirAll(dir, 0o700))

	// Old shape: no `schema`, no `workspaces`. A CLI written against schema 1
	// would have produced exactly this.
	legacyBlob := `{
  "authConfig": {"AuthToken":"legacy-tok","WorkspaceID":"legacy-ws","IsJWT":false},
  "credentials": {"auth_token":"legacy-tok","workspace_id":"legacy-ws","username":"erin"}
}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte(legacyBlob), 0o600))

	s := NewFile()
	got, err := s.Load()
	require.NoError(t, err)
	assert.Equal(t, "legacy-tok", got.AuthToken)
	assert.Equal(t, "legacy-ws", got.WorkspaceID)
	assert.Equal(t, "erin", got.Username)
	assert.Zero(t, got.SchemaVersion, "reader must treat absent schema key as v1, not the current version")
	assert.Nil(t, got.Workspaces, "schema-1 blob has no per-workspace map")

	// Round-trip: save without touching the fields.
	require.NoError(t, s.Save(got))

	after, err := s.Load()
	require.NoError(t, err)
	assert.Equal(t, auth.SchemaVersionCurrent, after.SchemaVersion, "the first write after read must upgrade the tag")
	assert.Equal(t, "legacy-tok", after.AuthToken, "credentials must survive the upgrade")
	assert.Equal(t, "legacy-ws", after.WorkspaceID)
	assert.Equal(t, "erin", after.Username)
}

func TestSaveWorkspaceToken_roundTripInFileStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	target := NewFile()
	require.NoError(t, target.Save(auth.TokenSet{AuthToken: "machine-tok", WorkspaceID: "machine-ws"}))

	require.NoError(t, SaveWorkspaceToken(target, "acme", auth.TokenSet{AuthToken: "acme-tok", WorkspaceID: "acme"}))
	require.NoError(t, SaveWorkspaceToken(target, "widgets", auth.TokenSet{AuthToken: "widgets-tok", WorkspaceID: "widgets"}))

	got, err := target.Load()
	require.NoError(t, err)
	assert.Equal(t, "machine-tok", got.AuthToken, "machine-wide fields must survive per-workspace writes")
	assert.Equal(t, "machine-ws", got.WorkspaceID)
	assert.Equal(t, auth.SchemaVersionCurrent, got.SchemaVersion)

	acme, ok := got.ForWorkspace("acme")
	require.True(t, ok)
	assert.Equal(t, "acme-tok", acme.AuthToken)

	widgets, ok := got.ForWorkspace("widgets")
	require.True(t, ok)
	assert.Equal(t, "widgets-tok", widgets.AuthToken)

	_, ok = got.ForWorkspace("missing")
	assert.False(t, ok, "unknown slug is a miss, not a zero-value hit")
}

func TestSaveWorkspaceToken_emptySlugErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	err := SaveWorkspaceToken(NewFile(), "  ", auth.TokenSet{AuthToken: "t"})
	require.Error(t, err)
}
