//go:build unit

package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/auth/keychain"
)

type fakeKeychain struct {
	stored   keychain.Credentials
	hasStore bool
	loadErr  error
	saveErr  error
	saved    *keychain.Credentials
}

func (f *fakeKeychain) Load() (keychain.Credentials, error) {
	if f.loadErr != nil {
		return keychain.Credentials{}, f.loadErr
	}
	if !f.hasStore {
		return keychain.Credentials{}, keychain.ErrNotFound
	}

	return f.stored, nil
}

func (f *fakeKeychain) Save(c keychain.Credentials) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	cp := c
	f.saved = &cp

	return nil
}

func TestLoadStartingCredentials_keychainWins(t *testing.T) {
	kc := &fakeKeychain{
		hasStore: true,
		stored:   keychain.Credentials{AuthToken: "kc-tok", WorkspaceID: "kc-ws"},
	}
	ws, tok, src := loadStartingCredentials(kc, "env-ws", "env-tok")
	assert.Equal(t, credsSourceKeychain, src)
	assert.Equal(t, "kc-ws", ws)
	assert.Equal(t, "kc-tok", tok)
}

func TestLoadStartingCredentials_envWhenKeychainEmpty(t *testing.T) {
	ws, tok, src := loadStartingCredentials(&fakeKeychain{}, "env-ws", "env-tok")
	assert.Equal(t, credsSourceEnv, src)
	assert.Equal(t, "env-ws", ws)
	assert.Equal(t, "env-tok", tok)
}

func TestLoadStartingCredentials_noneWhenNothingSet(t *testing.T) {
	ws, tok, src := loadStartingCredentials(&fakeKeychain{}, "", "")
	assert.Equal(t, credsSourceNone, src)
	assert.Empty(t, ws)
	assert.Empty(t, tok)
}

func TestLoadStartingCredentials_partialKeychainTreatedAsMissing(t *testing.T) {
	// Token without workspace ID is incomplete; should fall through to env.
	kc := &fakeKeychain{hasStore: true, stored: keychain.Credentials{AuthToken: "kc-tok"}}
	ws, tok, src := loadStartingCredentials(kc, "env-ws", "env-tok")
	assert.Equal(t, credsSourceEnv, src)
	assert.Equal(t, "env-ws", ws)
	assert.Equal(t, "env-tok", tok)
}

func TestActivateCmd_HasInteractiveFlag(t *testing.T) {
	flag := ActivateCmd.Flags().Lookup("interactive")
	require.NotNil(t, flag, "--interactive flag should be registered on activate command")
	assert.Equal(t, "false", flag.DefValue)
}
