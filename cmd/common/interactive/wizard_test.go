//go:build unit

package interactive

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
)

func TestActivateCmd_HasInteractiveFlag(t *testing.T) {
	flag := common.ActivateCmd.Flags().Lookup("interactive")
	require.NotNil(t, flag, "--interactive flag should be registered on activate command")
	assert.Equal(t, "false", flag.DefValue)
}

func TestPersistCredentials_writesUsernameField(t *testing.T) {
	kc := &stubKeychain{}
	require.NoError(t, persistCredentials(kc, authpkg.TokenSet{}, "ws-1", "tok-1", "alice"))
	assert.Equal(t, "alice", kc.saved.Username)
	assert.Equal(t, "ws-1", kc.saved.WorkspaceID)
	assert.Equal(t, "tok-1", kc.saved.AuthToken)
}

type stubKeychain struct {
	creds authpkg.TokenSet
	saved authpkg.TokenSet
}

func (s *stubKeychain) Load() (authpkg.TokenSet, error) {
	return s.creds, nil
}

func (s *stubKeychain) Save(c authpkg.TokenSet) error {
	s.saved = c

	return nil
}

func (s *stubKeychain) Backend() authpkg.Backend { return authpkg.BackendKeychain }
func (s *stubKeychain) Clear() error             { return nil }

func TestPersistCredentials_preservesOAuthFieldsOnUpdate(t *testing.T) {
	existing := authpkg.TokenSet{
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
	assert.True(t, usernamePersistable(authpkg.Origin{Backend: authpkg.BackendKeychain}))
	assert.True(t, usernamePersistable(authpkg.Origin{Backend: authpkg.BackendEnv}))
	assert.True(t, usernamePersistable(authpkg.Origin{}))
	assert.True(t, usernamePersistable(authpkg.Origin{Backend: authpkg.BackendFile, Provenance: authpkg.ProvenanceLegacy}))
	assert.False(t, usernamePersistable(authpkg.Origin{Backend: authpkg.BackendJWT}))
}

func TestDebugFlag_ORsGlobal_ActivateInteractive(t *testing.T) {
	t.Cleanup(func() { common.IsDebugLogMode = false })

	common.IsDebugLogMode = true
	params := struct{ DebugLogging bool }{DebugLogging: false}

	params.DebugLogging = common.DebugEnabled(params.DebugLogging)

	assert.True(t, params.DebugLogging, "global -d must OR into interactive params.DebugLogging")
}
