//go:build unit

package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

type memStore struct {
	creds   keychain.Credentials
	present bool
	kind    Kind
}

func (m *memStore) Kind() Kind { return m.kind }

func (m *memStore) Load() (keychain.Credentials, error) {
	if !m.present {
		return keychain.Credentials{}, ErrNotFound
	}

	return m.creds, nil
}

func (m *memStore) Save(c keychain.Credentials) error {
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

// A different token is a different credential, so OAuth fields that described
// the old one must not be carried over.
func TestMergeActivateCreds_DropsOAuthFieldsForADifferentToken(t *testing.T) {
	s := &memStore{kind: KindKeychain, present: true, creds: keychain.Credentials{
		AuthToken: "pat-1", WorkspaceID: "ws-1", RefreshToken: "refresh-1", JWT: "jwt-1",
	}}

	got := mergeActivateCreds(s, configcommon.CacheAuthConfig{AuthToken: "pat-2", WorkspaceID: "ws-1"})

	assert.Equal(t, "pat-2", got.AuthToken)
	assert.Empty(t, got.RefreshToken)
	assert.Empty(t, got.JWT)
	assert.False(t, got.IsOAuthManaged())
}

func TestMergeActivateCreds_EmptyStore(t *testing.T) {
	s := &memStore{kind: KindKeychain}

	got := mergeActivateCreds(s, configcommon.CacheAuthConfig{AuthToken: "pat-1", WorkspaceID: "ws-1"})

	assert.Equal(t, "pat-1", got.AuthToken)
	assert.Equal(t, "ws-1", got.WorkspaceID)
}
