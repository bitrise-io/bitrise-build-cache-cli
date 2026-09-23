//go:build unit

package live

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

// brokerEnvs is what a Build Hub runner exports.
func brokerEnvs() map[string]string {
	return map[string]string{
		auth.EnvBuildHubVMToken:    "vm-token-value",
		auth.EnvBuildHubVMTokenURL: "https://api.bitrise.io/build_hub/vm_tokens",
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

// A Build Hub runner can always broker, so a stale login left on the machine must
// not shadow the credential the runner is offering.
func TestResolve_BrokeringBeatsTheStores(t *testing.T) {
	stored := &fakeStore{
		backend: auth.BackendKeychain,
		present: true,
		ts:      auth.TokenSet{AuthToken: "stale-pat", WorkspaceID: "stale-org"},
	}
	r := &Resolver{Broker: brokerReturning(t, "brokered-org", nil), Backends: []store.Store{stored}}

	cred, origin, err := r.Resolve(context.Background(), brokerEnvs())

	require.NoError(t, err)
	assert.Equal(t, "brokered-org", cred.WorkspaceID)
	assert.Equal(t, auth.ProvenanceBrokered, origin.Provenance)
}

// A broker outage degrades to whatever the machine already had, rather than taking
// the build down.
func TestResolve_BrokerFailureFallsThroughToTheStores(t *testing.T) {
	stored := &fakeStore{
		backend: auth.BackendKeychain,
		present: true,
		ts:      auth.TokenSet{AuthToken: "stored-pat", WorkspaceID: "stored-org"},
	}
	r := &Resolver{
		Broker: func(context.Context, map[string]string) (auth.Credential, error) {
			return auth.Credential{}, errors.New("instance-manager unavailable")
		},
		Backends: []store.Store{stored},
	}

	cred, origin, err := r.Resolve(context.Background(), brokerEnvs())

	require.NoError(t, err)
	assert.Equal(t, "stored-pat", cred.Token)
	assert.Equal(t, auth.BackendKeychain, origin.Backend)
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
