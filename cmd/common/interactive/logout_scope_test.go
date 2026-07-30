//go:build unit

package interactive

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/oauth"
)

// logout is scoped to the browser login, so a manually set token in the other
// backend has to survive it. oauth.Clear() wipes both, which is why logout
// targets only the store the login was found in.
func TestLogout_LeavesAManualTokenInTheOtherBackend(t *testing.T) {
	login := &failingStore{kind: store.KindKeychain}
	require.NoError(t, login.Save(keychain.Credentials{
		AuthToken: "oauth-pat", WorkspaceID: "ws-1", RefreshToken: "refresh-1",
	}))

	manual := &failingStore{kind: store.KindFile}
	require.NoError(t, manual.Save(keychain.Credentials{AuthToken: "manual-pat", WorkspaceID: "ws-2"}))

	require.NoError(t, oauth.ClearFrom(login))

	_, err := login.Load()
	assert.ErrorIs(t, err, store.ErrNotFound, "the login should be gone")

	kept, err := manual.Load()
	require.NoError(t, err)
	assert.Equal(t, "manual-pat", kept.AuthToken, "the manually set token must survive logout")
}
