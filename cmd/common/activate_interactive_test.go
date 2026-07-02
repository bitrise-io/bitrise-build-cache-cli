//go:build unit

package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/auth/keychain"
)

func TestActivateCmd_HasInteractiveFlag(t *testing.T) {
	flag := ActivateCmd.Flags().Lookup("interactive")
	require.NotNil(t, flag, "--interactive flag should be registered on activate command")
	assert.Equal(t, "false", flag.DefValue)
}

func TestPersistCredentials_writesUsernameField(t *testing.T) {
	kc := &stubKeychain{}
	require.NoError(t, persistCredentials(kc, "ws-1", "tok-1", "alice"))
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
