//go:build unit

package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

func TestActivateCmd_HasInteractiveFlag(t *testing.T) {
	flag := ActivateCmd.Flags().Lookup("interactive")
	require.NotNil(t, flag, "--interactive flag should be registered on activate command")
	assert.Equal(t, "false", flag.DefValue)
}

func TestPersistCredentials_writesUsernameField(t *testing.T) {
	kc := &stubKeychain{}
	require.NoError(t, persistCredentials(kc, keychain.Credentials{}, "ws-1", "tok-1", "alice"))
	assert.Equal(t, "alice", kc.saved.Username)
	assert.Equal(t, "ws-1", kc.saved.WorkspaceID)
	assert.Equal(t, "tok-1", kc.saved.AuthToken)
}

type stubKeychain struct {
	creds keychain.Credentials
	saved keychain.Credentials
}

func (s *stubKeychain) Load() (keychain.Credentials, error) {
	return s.creds, nil
}

func (s *stubKeychain) Save(c keychain.Credentials) error {
	s.saved = c

	return nil
}

func TestPersistCredentials_preservesOAuthFieldsOnUpdate(t *testing.T) {
	existing := keychain.Credentials{
		AuthToken:    "old-tok",
		WorkspaceID:  "old-ws",
		RefreshToken: "refresh-abc",
		JWT:          "jwt-xyz",
	}
	kc := &stubKeychain{}

	require.NoError(t, persistCredentials(kc, existing, "old-ws", "old-tok", "alice"))
	assert.Equal(t, "alice", kc.saved.Username)
	assert.Equal(t, "refresh-abc", kc.saved.RefreshToken, "OAuth refresh token must survive username edit")
	assert.Equal(t, "jwt-xyz", kc.saved.JWT)
}

func TestUsernamePersistable(t *testing.T) {
	assert.True(t, usernamePersistable(configcommon.AuthSourceKeychain))
	assert.True(t, usernamePersistable(configcommon.AuthSourceEnvVars))
	assert.True(t, usernamePersistable(configcommon.AuthSourceNone))
	assert.True(t, usernamePersistable(configcommon.AuthSourceMultiplatform))
	assert.False(t, usernamePersistable(configcommon.AuthSourceJWT))
}
