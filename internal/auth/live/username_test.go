//go:build unit

package live

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

func usernameResolver(stored auth.TokenSet, present bool) *Resolver {
	return &Resolver{Backends: []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: stored, present: present}}}
}

func TestResolveUsername_EnvWins(t *testing.T) {
	r := usernameResolver(auth.TokenSet{AuthToken: "t", WorkspaceID: "w", Username: "stored"}, true)

	name, src := r.ResolveUsername(map[string]string{auth.EnvUsername: "alice-dev"})

	assert.Equal(t, "alice-dev", name)
	assert.Equal(t, UsernameSourceEnv, src)
}

// A display name is written by `auth username`, independently of the token, so it
// has to be found even when the credential itself came from the environment.
func TestResolveUsername_FallsBackToTheStoredRecord(t *testing.T) {
	r := usernameResolver(auth.TokenSet{AuthToken: "t", WorkspaceID: "w", Username: "jane"}, true)

	name, src := r.ResolveUsername(map[string]string{})

	assert.Equal(t, "jane", name)
	assert.Equal(t, UsernameSourceStored, src)
}

func TestResolveUsername_WhitespaceEnvFallsThrough(t *testing.T) {
	r := usernameResolver(auth.TokenSet{AuthToken: "t", WorkspaceID: "w", Username: "jane"}, true)

	name, src := r.ResolveUsername(map[string]string{auth.EnvUsername: "   "})

	assert.Equal(t, "jane", name)
	assert.Equal(t, UsernameSourceStored, src)
}

func TestResolveUsername_FallsBackToTheOSUser(t *testing.T) {
	r := usernameResolver(auth.TokenSet{}, false)

	name, src := r.ResolveUsername(map[string]string{})

	assert.Equal(t, UsernameSourceOS, src)
	assert.NotEmpty(t, name)
}

// Resolve must not pay for the store read: Bound.Get is the proxy's per-RPC path.
func TestResolve_DoesNotLookUpTheDisplayName(t *testing.T) {
	stored := &fakeStore{backend: auth.BackendKeychain, ts: auth.TokenSet{AuthToken: "t", WorkspaceID: "w", Username: "jane"}, present: true}
	r := &Resolver{
		Backends:       []store.Store{stored},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
	}

	_, _, err := r.ResolveNoRefresh(envVars())

	assert.NoError(t, err)
	assert.Zero(t, stored.loadHits, "the env path must not touch the store")
}
