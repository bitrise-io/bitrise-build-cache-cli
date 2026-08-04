//go:build unit

package store

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/multiplatform"
)

func newTestLogger() log.Logger { return log.NewLogger(log.WithOutput(io.Discard)) }

type memStore struct {
	creds   keychain.Credentials
	present bool
	kind    Kind
	saveErr error
}

func (m *memStore) Kind() Kind { return m.kind }

func (m *memStore) Load() (keychain.Credentials, error) {
	if !m.present {
		return keychain.Credentials{}, ErrNotFound
	}

	return m.creds, nil
}

func (m *memStore) Save(c keychain.Credentials) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.creds, m.present = c, true

	return nil
}

func (m *memStore) Clear() error {
	m.creds, m.present = keychain.Credentials{}, false

	return nil
}

// Re-persisting the same token during activate must not strip the OAuth fields:
// losing the refresh token breaks `auth logout` and leaves the login unable to
// refresh, degrading it into a bare short-lived PAT.
func TestMergeActivateCreds_KeepsOAuthFieldsForTheSameToken(t *testing.T) {
	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	s := &memStore{kind: KindKeychain, present: true, creds: keychain.Credentials{
		AuthToken:    "pat-1",
		WorkspaceID:  "ws-1",
		Username:     "alice",
		RefreshToken: "refresh-1",
		JWT:          "jwt-1",
		PATExpiry:    expiry,
	}}

	got := mergeActivateCreds(s, configcommon.CacheAuthConfig{AuthToken: "pat-1", WorkspaceID: "ws-1"})

	assert.Equal(t, "refresh-1", got.RefreshToken)
	assert.Equal(t, "jwt-1", got.JWT)
	assert.Equal(t, "alice", got.Username, "the display-name override must survive too")
	assert.Equal(t, expiry, got.PATExpiry)
	require.True(t, got.IsOAuthManaged())
}

// A workspace switch keeps the login: the refresh token is user-scoped.
func TestMergeActivateCreds_KeepsOAuthFieldsAcrossWorkspaceChange(t *testing.T) {
	s := &memStore{kind: KindKeychain, present: true, creds: keychain.Credentials{
		AuthToken: "pat-1", WorkspaceID: "ws-1", RefreshToken: "refresh-1",
	}}

	got := mergeActivateCreds(s, configcommon.CacheAuthConfig{AuthToken: "pat-1", WorkspaceID: "ws-2"})

	assert.Equal(t, "ws-2", got.WorkspaceID)
	assert.Equal(t, "refresh-1", got.RefreshToken)
}

// A refreshed PAT is the same credential. The stored token routinely differs from
// the resolved one — the login mints short-lived PATs and refreshes them — so
// comparing tokens here is what discarded the refresh token and killed the login
// at expiry. activate re-persists; it does not redefine.
func TestMergeActivateCreds_KeepsOAuthFieldsWhenTheTokenWasRefreshed(t *testing.T) {
	s := &memStore{kind: KindKeychain, present: true, creds: keychain.Credentials{
		AuthToken: "pat-1", WorkspaceID: "ws-1", RefreshToken: "refresh-1", JWT: "jwt-1", Username: "dev",
	}}

	got := mergeActivateCreds(s, configcommon.CacheAuthConfig{AuthToken: "pat-2", WorkspaceID: "ws-1"})

	assert.Equal(t, "pat-2", got.AuthToken, "the freshly resolved token wins")
	assert.Equal(t, "refresh-1", got.RefreshToken, "without this the login cannot refresh again")
	assert.Equal(t, "jwt-1", got.JWT)
	assert.Equal(t, "dev", got.Username)
	assert.True(t, got.IsOAuthManaged())
}

func TestMergeActivateCreds_EmptyStore(t *testing.T) {
	s := &memStore{kind: KindKeychain}

	got := mergeActivateCreds(s, configcommon.CacheAuthConfig{AuthToken: "pat-1", WorkspaceID: "ws-1"})

	assert.Equal(t, "pat-1", got.AuthToken)
	assert.Equal(t, "ws-1", got.WorkspaceID)
}

// A host with no usable OS keychain (headless Linux, containers) is where the
// browser login lands in the config file. Activation must not overwrite that with
// the 3-field AuthConfig shape: the refresh token lives only in Credentials, and
// without it the login degrades to a bare PAT that dies at expiry with nothing
// able to renew it. Observed on an RDE — `activate` ran, the refresh token
// vanished, and the storage helper later refused to start on `unauthenticated`.
func TestPersistActivateCreds_KeychainUnusableKeepsTheRefreshToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	login := keychain.Credentials{
		AuthToken:          "bitpat_minted",
		WorkspaceID:        "ws-1",
		RefreshToken:       "refresh-me",
		RefreshTokenExpiry: time.Now().Add(24 * time.Hour),
		Username:           "dev",
	}
	require.NoError(t, NewFile().Save(login))

	// A keychain that refuses every write, as a host with no secret-service has.
	deadKeychain := &memStore{kind: KindKeychain, saveErr: errors.New("no usable OS keychain on this machine")}

	var mpCfg multiplatformconfig.Config
	persistActivateCredsTo(
		newTestLogger(),
		deadKeychain,
		configcommon.CacheAuthConfig{AuthToken: "bitpat_minted", WorkspaceID: "ws-1"},
		&mpCfg,
	)

	require.NotNil(t, mpCfg.Credentials, "the full credential must be written, not just AuthConfig")
	assert.Equal(t, "refresh-me", mpCfg.Credentials.RefreshToken, "without this the login cannot refresh")
	assert.Equal(t, "dev", mpCfg.Credentials.Username)
	assert.Equal(t, "bitpat_minted", mpCfg.AuthConfig.AuthToken, "downstream readers still use AuthConfig")
}
