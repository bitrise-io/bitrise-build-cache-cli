//go:build unit

package live

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

func pinResolver(target *fakeStore) *Resolver {
	return &Resolver{
		Backends:   []store.Store{target},
		LegacyFile: func() (auth.Credential, bool) { return auth.Credential{}, false },
	}
}

// The pin is read-modify-write: a refresh token or display name already on disk
// must survive an activation that only knows a token and a workspace.
func TestResolvePinned_PreservesTheExistingRecord(t *testing.T) {
	target := &fakeStore{
		backend: auth.BackendFile,
		present: true,
		ts:      auth.TokenSet{AuthToken: "old", WorkspaceID: "old-ws", Username: "bob", RefreshToken: "rt"},
	}
	// Env wins resolution; the pin then writes through to the same record.
	_, _, err := pinResolver(target).ResolvePinned(t.Context(), envVars(), true)
	require.NoError(t, err)

	require.NotNil(t, target.saved)
	assert.Equal(t, envToken, target.saved.AuthToken)
	assert.Equal(t, envWS, target.saved.WorkspaceID)
	assert.Equal(t, "bob", target.saved.Username, "display name must survive the pin")
	assert.Equal(t, "rt", target.saved.RefreshToken, "refresh token must survive the pin")
}

// A credential that already lives in a store is where it needs to be.
func TestResolvePinned_DoesNotRewriteAStoredCredential(t *testing.T) {
	target := &fakeStore{backend: auth.BackendKeychain, present: true, ts: keychainTS()}
	r := pinResolver(target)

	_, origin, err := r.ResolvePinned(t.Context(), map[string]string{}, false)

	require.NoError(t, err)
	assert.Equal(t, auth.BackendKeychain, origin.Backend)
	assert.Nil(t, target.saved, "an already-stored credential must not be written back")
}

// The reported origin is where the credential was resolved from, not where the pin
// happened to put a copy: status and doctor must say "environment variables".
func TestResolvePinned_ReportsTheResolvedOrigin(t *testing.T) {
	target := &fakeStore{backend: auth.BackendFile}

	_, origin, err := pinResolver(target).ResolvePinned(t.Context(), envVars(), true)

	require.NoError(t, err)
	assert.Equal(t, auth.BackendEnv, origin.Backend)
}

// A failed write is not a failed activation.
func TestResolvePinned_SurvivesAnUnwritableStore(t *testing.T) {
	target := &fakeStore{backend: auth.BackendFile, saveErr: assert.AnError}

	cred, _, err := pinResolver(target).ResolvePinned(t.Context(), envVars(), true)

	require.NoError(t, err)
	assert.Equal(t, envToken, cred.Token)
}
