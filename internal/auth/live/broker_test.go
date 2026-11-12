//go:build unit

package live

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

func rdeEnvs() map[string]string {
	return map[string]string{
		auth.EnvVMToken:    "vm-token-value",
		auth.EnvVMTokenURL: "https://instance-manager.services.bitrise.io/vm/tokens",
	}
}

// brokerEnvs is what a Build Hub runner exports.
func brokerEnvs() map[string]string {
	return map[string]string{
		auth.EnvBuildHubVMToken:    "vm-token-value",
		auth.EnvBuildHubVMTokenURL: "https://api.bitrise.io/build-hub/vm/tokens",
	}
}

func brokerReturning(t *testing.T, workspaceID string, calls *int) func(context.Context, map[string]string) (auth.Credential, error) {
	t.Helper()

	return func(_ context.Context, _ map[string]string) (auth.Credential, error) {
		if calls != nil {
			*calls++
		}

		return auth.Credential{
			Token:       umaJWT(t, workspaceID),
			WorkspaceID: workspaceID,
			Expiry:      time.Now().Add(time.Hour),
		}, nil
	}
}

// On a Build Hub runner with nothing else configured, the CLI brokers its own
// credential rather than failing for want of a token.
func TestResolve_BrokersWhenNothingElseIsConfigured(t *testing.T) {
	r := &Resolver{Broker: brokerReturning(t, "org-slug", nil), Backends: []store.Store{}}

	cred, origin, err := r.Resolve(context.Background(), brokerEnvs())

	require.NoError(t, err)
	assert.Equal(t, "org-slug", cred.WorkspaceID)
	assert.Equal(t, auth.BackendJWT, origin.Backend)
	assert.Equal(t, auth.ProvenanceBrokered, origin.Provenance)
	assert.False(t, cred.Expiry.IsZero(), "a brokered credential carries the issuer's expiry")
}

// An explicitly configured token is the user's deliberate choice and must keep
// beating anything the runner offers.
func TestResolve_ExplicitEnvTokenBeatsBrokering(t *testing.T) {
	calls := 0
	envs := brokerEnvs()
	envs[auth.EnvAuthToken] = "explicit-pat"
	envs[auth.EnvWorkspaceID] = "explicit-org"

	r := &Resolver{Broker: brokerReturning(t, "brokered-org", &calls), Backends: []store.Store{}}

	cred, origin, err := r.Resolve(context.Background(), envs)

	require.NoError(t, err)
	assert.Equal(t, "explicit-pat", cred.Token)
	assert.Equal(t, auth.BackendEnv, origin.Backend)
	assert.Zero(t, calls, "brokering must not even be attempted")
}

// Bitrise CI injects the JWT directly; brokering is for runners that have none.
func TestResolve_StaticCIJWTBeatsBrokering(t *testing.T) {
	calls := 0
	envs := brokerEnvs()
	envs[auth.EnvJWT] = umaJWT(t, "ci-org")

	r := &Resolver{Broker: brokerReturning(t, "brokered-org", &calls), Backends: []store.Store{}}

	cred, origin, err := r.Resolve(context.Background(), envs)

	require.NoError(t, err)
	assert.Equal(t, "ci-org", cred.WorkspaceID)
	assert.Equal(t, auth.ProvenanceInjected, origin.Provenance)
	assert.Zero(t, calls, "brokering must not even be attempted")
}

// The VM token is a convenience the machine offers; a login or `auth set` token on it
// is the user's explicit choice and must win, without even attempting the exchange.
func TestResolve_StoredCredentialBeatsBrokering(t *testing.T) {
	for _, stored := range []*fakeStore{
		{backend: auth.BackendKeychain, present: true, ts: auth.TokenSet{AuthToken: "stored-pat", WorkspaceID: "stored-org"}},
		{backend: auth.BackendFile, present: true, ts: auth.TokenSet{AuthToken: "stored-pat", WorkspaceID: "stored-org"}},
	} {
		calls := 0
		r := &Resolver{Broker: brokerReturning(t, "brokered-org", &calls), Backends: []store.Store{stored}}

		cred, origin, err := r.Resolve(context.Background(), brokerEnvs())

		require.NoError(t, err)
		assert.Equal(t, "stored-pat", cred.Token)
		assert.Equal(t, stored.backend, origin.Backend)
		assert.Zero(t, calls, "brokering must not even be attempted")

		cred, _, err = r.Resolve(context.Background(), rdeEnvs())
		require.NoError(t, err)
		assert.Equal(t, "stored-pat", cred.Token)
		assert.Zero(t, calls, "the neutral VM pair yields to the stores too")
	}

	calls := 0
	r := &Resolver{Broker: brokerReturning(t, "brokered-org", &calls), Backends: []store.Store{&fakeStore{backend: auth.BackendKeychain}}}
	_, origin, err := r.Resolve(context.Background(), rdeEnvs())
	require.NoError(t, err)
	assert.Equal(t, auth.ProvenanceBrokered, origin.Provenance, "with nothing stored the VM token is used")
	assert.Equal(t, 1, calls)
}

// A login must not have a brokered token pinned over it for the processes that
// never see the VM env.
func TestResolvePinned_StoredCredentialPinsNoBrokeredToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := 0
	stored := &fakeStore{backend: auth.BackendKeychain, present: true, ts: auth.TokenSet{AuthToken: "stored-pat", WorkspaceID: "stored-org"}}
	r := &Resolver{Broker: brokerReturning(t, "brokered-org", &calls), Backends: []store.Store{stored}}

	_, origin, err := r.ResolvePinned(context.Background(), rdeEnvs(), false)

	require.NoError(t, err)
	assert.Equal(t, auth.BackendKeychain, origin.Backend)
	assert.Zero(t, calls)
	_, _, pinned := readAnalyticsCredential()
	assert.False(t, pinned)
}

// A broker outage falls through to the pinned copy, rather than taking the build down,
// and does not replace the usual error when nothing else resolves.
func TestResolve_BrokerFailureFallsThrough(t *testing.T) {
	failing := func(context.Context, map[string]string) (auth.Credential, error) {
		return auth.Credential{}, errors.New("instance-manager unavailable")
	}
	pinned := auth.Credential{Token: umaJWT(t, "pinned-org"), WorkspaceID: "pinned-org"}
	r := &Resolver{
		Broker:   failing,
		Backends: []store.Store{},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) {
			return pinned, auth.Origin{Backend: auth.BackendJWT, Provenance: auth.ProvenanceBrokered}, true
		},
	}

	cred, _, err := r.Resolve(context.Background(), brokerEnvs())
	require.NoError(t, err)
	assert.Equal(t, pinned.Token, cred.Token)

	r.AnalyticsBlock = func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false }
	_, _, err = r.Resolve(context.Background(), brokerEnvs())
	require.ErrorIs(t, err, auth.ErrTokenNotProvided)
}

// ResolveNoRefresh is documented as making no network call, and `status` and the
// doctor rely on that. Brokering is a network call, so it must not happen here.
func TestResolveNoRefresh_NeverBrokers(t *testing.T) {
	calls := 0
	stored := &fakeStore{
		backend: auth.BackendKeychain,
		present: true,
		ts:      auth.TokenSet{AuthToken: "stored-pat", WorkspaceID: "stored-org"},
	}
	r := &Resolver{Broker: brokerReturning(t, "brokered-org", &calls), Backends: []store.Store{stored}}

	cred, origin, err := r.ResolveNoRefresh(brokerEnvs())

	require.NoError(t, err)
	assert.Zero(t, calls, "the offline path must not broker")
	assert.Equal(t, "stored-pat", cred.Token)
	assert.Equal(t, auth.BackendKeychain, origin.Backend)
}

// The origin is what `status` and the doctor print, and a brokered token has to be
// distinguishable from the CI JWT it shares a backend with.
func TestBrokeredOriginIsDistinctFromTheCIJWT(t *testing.T) {
	brokered := auth.Origin{Backend: auth.BackendJWT, Provenance: auth.ProvenanceBrokered}
	injected := auth.Origin{Backend: auth.BackendJWT, Provenance: auth.ProvenanceInjected}

	assert.NotEqual(t, brokered.Label(), injected.Label())
	assert.NotEqual(t, brokered.ShortLabel(), injected.ShortLabel())
	assert.False(t, brokered.StoreManaged(), "a brokered token has no store to refresh from")
}

// The offline callers name the brokered token only where Resolve would actually
// reach the exchange.
func TestBrokeringEnv(t *testing.T) {
	rde := map[string]string{auth.EnvVMToken: "vm-token", auth.EnvVMTokenURL: "https://instance-manager.services.bitrise.io/vm/tokens"}
	with := func(extra map[string]string) map[string]string {
		envs := maps.Clone(rde)
		maps.Copy(envs, extra)

		return envs
	}
	oidc := map[string]string{
		auth.EnvOIDCPolicyID:           "policy",
		auth.EnvWorkspaceID:            "ws",
		auth.EnvGitHubOIDCRequestURL:   "https://token.actions.githubusercontent.com",
		auth.EnvGitHubOIDCRequestToken: "gh-token",
	}
	without := func(keys ...string) map[string]string {
		envs := with(oidc)
		for _, k := range keys {
			delete(envs, k)
		}

		return envs
	}

	for _, tc := range []struct {
		name string
		envs map[string]string
		want string
	}{
		{"RDE pair", rde, auth.EnvVMToken},
		{"Build Hub pair", brokerEnvs(), auth.EnvBuildHubVMToken},
		{"no pair", map[string]string{}, ""},
		{"injected JWT wins", with(map[string]string{auth.EnvJWT: "jwt"}), ""},
		{"env token wins", with(map[string]string{auth.EnvAuthToken: "pat", auth.EnvWorkspaceID: "ws"}), ""},
		{"OIDC goes first", with(oidc), ""},
		{"policy set, no GitHub token", without(auth.EnvGitHubOIDCRequestToken), auth.EnvVMToken},
		{"policy set, no workspace", without(auth.EnvWorkspaceID), auth.EnvVMToken},
	} {
		assert.Equal(t, tc.want, (&Resolver{Backends: []store.Store{}}).BrokeringEnv(tc.envs), tc.name)
	}

	stored := &fakeStore{backend: auth.BackendFile, present: true, ts: auth.TokenSet{AuthToken: "pat", WorkspaceID: "ws"}}
	assert.Empty(t, (&Resolver{Backends: []store.Store{stored}}).BrokeringEnv(rde), "a stored credential wins")
	noWorkspace := &fakeStore{backend: auth.BackendKeychain, present: true, ts: auth.TokenSet{AuthToken: "pat"}}
	assert.Equal(t, auth.EnvVMToken, (&Resolver{Backends: []store.Store{noWorkspace}}).BrokeringEnv(rde), "an unusable login does not")
}
