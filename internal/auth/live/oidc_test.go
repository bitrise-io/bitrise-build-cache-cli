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
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// oidcEnvs is a GitHub Actions job with `id-token: write`, a trust policy and a
// workspace configured, and no token.
func oidcEnvs() map[string]string {
	return map[string]string{
		auth.EnvOIDCPolicyID:           "policy-uuid",
		auth.EnvWorkspaceID:            "oidc-org",
		auth.EnvGitHubOIDCRequestURL:   "https://example.com/idtoken",
		auth.EnvGitHubOIDCRequestToken: "request-token",
	}
}

func oidcReturning(calls *int) func(context.Context, map[string]string) (auth.Credential, error) {
	return func(_ context.Context, envs map[string]string) (auth.Credential, error) {
		if calls != nil {
			*calls++
		}

		return auth.Credential{Token: "oidc-wat", WorkspaceID: envs[auth.EnvWorkspaceID], Expiry: time.Now().Add(15 * time.Minute)}, nil
	}
}

func TestResolve_ExchangesTheGitHubOIDCTokenWhenNothingElseIsConfigured(t *testing.T) {
	r := &Resolver{OIDC: oidcReturning(nil), Backends: []store.Store{}}

	cred, origin, err := r.Resolve(context.Background(), oidcEnvs())

	require.NoError(t, err)
	assert.Equal(t, "oidc-wat", cred.Token)
	assert.Equal(t, "oidc-org", cred.WorkspaceID)
	assert.False(t, cred.Expiry.IsZero(), "an exchanged credential carries the server's expiry")
	assert.Equal(t, auth.Origin{Backend: auth.BackendEnv, Provenance: auth.ProvenanceOIDC}, origin)
}

// The OIDC setup sets the workspace but no token, and must not be mistaken for
// the explicit env-var pair.
func TestResolve_WorkspaceAloneIsNotAnExplicitToken(t *testing.T) {
	calls := 0
	r := &Resolver{OIDC: oidcReturning(&calls), Backends: []store.Store{}}

	_, origin, err := r.Resolve(context.Background(), oidcEnvs())

	require.NoError(t, err)
	assert.Equal(t, 1, calls)
	assert.Equal(t, auth.ProvenanceOIDC, origin.Provenance)
}

func TestResolve_ExplicitEnvTokenBeatsOIDC(t *testing.T) {
	calls := 0
	envs := oidcEnvs()
	envs[auth.EnvAuthToken] = "explicit-pat"

	r := &Resolver{OIDC: oidcReturning(&calls), Backends: []store.Store{}}

	cred, origin, err := r.Resolve(context.Background(), envs)

	require.NoError(t, err)
	assert.Equal(t, "explicit-pat", cred.Token)
	assert.Equal(t, auth.BackendEnv, origin.Backend)
	assert.Equal(t, auth.ProvenanceInjected, origin.Provenance)
	assert.Zero(t, calls, "the exchange must not even be attempted")
}

func TestResolve_CIJWTBeatsOIDC(t *testing.T) {
	calls := 0
	envs := oidcEnvs()
	envs[auth.EnvJWT] = umaJWT(t, "ci-org")

	r := &Resolver{OIDC: oidcReturning(&calls), Backends: []store.Store{}}

	cred, _, err := r.Resolve(context.Background(), envs)

	require.NoError(t, err)
	assert.Equal(t, "ci-org", cred.WorkspaceID)
	assert.Zero(t, calls)
}

// A trust policy is something the user configured; the Build Hub token is offered
// by the runner regardless.
func TestResolve_OIDCBeatsBuildHubBrokering(t *testing.T) {
	brokerCalls := 0
	envs := oidcEnvs()
	for k, v := range brokerEnvs() {
		envs[k] = v
	}

	r := &Resolver{OIDC: oidcReturning(nil), Broker: brokerReturning(t, "brokered-org", &brokerCalls), Backends: []store.Store{}}

	_, origin, err := r.Resolve(context.Background(), envs)

	require.NoError(t, err)
	assert.Equal(t, auth.ProvenanceOIDC, origin.Provenance)
	assert.Zero(t, brokerCalls)
}

func TestResolve_OIDCBeatsTheStores(t *testing.T) {
	stored := &fakeStore{
		backend: auth.BackendKeychain,
		present: true,
		ts:      auth.TokenSet{AuthToken: "stale-pat", WorkspaceID: "stale-org"},
	}
	r := &Resolver{OIDC: oidcReturning(nil), Backends: []store.Store{stored}}

	cred, _, err := r.Resolve(context.Background(), oidcEnvs())

	require.NoError(t, err)
	assert.Equal(t, "oidc-wat", cred.Token)
}

// A rejected exchange degrades to whatever else is available rather than taking
// the build down.
func TestResolve_OIDCFailureFallsThroughToBrokeringThenTheStores(t *testing.T) {
	failing := func(context.Context, map[string]string) (auth.Credential, error) {
		return auth.Credential{}, errors.New("invalid_token")
	}
	envs := oidcEnvs()
	for k, v := range brokerEnvs() {
		envs[k] = v
	}

	r := &Resolver{OIDC: failing, Broker: brokerReturning(t, "brokered-org", nil), Backends: []store.Store{}}
	_, origin, err := r.Resolve(context.Background(), envs)
	require.NoError(t, err)
	assert.Equal(t, auth.ProvenanceBrokered, origin.Provenance)

	stored := &fakeStore{backend: auth.BackendKeychain, present: true, ts: auth.TokenSet{AuthToken: "stored-pat", WorkspaceID: "stored-org"}}
	r = &Resolver{OIDC: failing, Backends: []store.Store{stored}}
	cred, _, err := r.Resolve(context.Background(), oidcEnvs())
	require.NoError(t, err)
	assert.Equal(t, "stored-pat", cred.Token)
}

// The exchanged token is opaque, so there is no workspace to read from it; minting
// one that cannot be used would only cost a token server-side.
func TestResolve_OIDCWithoutWorkspaceDoesNotExchange(t *testing.T) {
	calls := 0
	envs := oidcEnvs()
	delete(envs, auth.EnvWorkspaceID)

	r := &Resolver{OIDC: oidcReturning(&calls), Backends: []store.Store{}}
	_, _, err := r.Resolve(context.Background(), envs)

	require.Error(t, err)
	assert.Zero(t, calls)
}

// Without `id-token: write` GitHub offers no token: the real client is never built,
// and resolution falls through instead of failing on a request that cannot work.
func TestResolve_OIDCWithoutGitHubTokenFallsThrough(t *testing.T) {
	envs := oidcEnvs()
	delete(envs, auth.EnvGitHubOIDCRequestURL)
	delete(envs, auth.EnvGitHubOIDCRequestToken)
	stored := &fakeStore{backend: auth.BackendKeychain, present: true, ts: auth.TokenSet{AuthToken: "stored-pat", WorkspaceID: "stored-org"}}

	r := &Resolver{Backends: []store.Store{stored}}
	cred, _, err := r.Resolve(context.Background(), envs)

	require.NoError(t, err)
	assert.Equal(t, "stored-pat", cred.Token)
}

// ResolveNoRefresh is documented as making no network call; the exchange is one.
func TestResolveNoRefresh_NeverExchanges(t *testing.T) {
	calls := 0
	r := &Resolver{
		OIDC:           oidcReturning(&calls),
		Backends:       []store.Store{},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
	}

	_, _, _ = r.ResolveNoRefresh(oidcEnvs())

	assert.Zero(t, calls)
}

// The token is a WAT, not a JWT: Gradle needs it workspace-prefixed.
func TestGradleToken_PrefixesAnOIDCCredential(t *testing.T) {
	r := &Resolver{OIDC: oidcReturning(nil), Backends: []store.Store{}}

	cred, origin, err := r.Resolve(context.Background(), oidcEnvs())

	require.NoError(t, err)
	assert.Equal(t, "oidc-org:oidc-wat", auth.GradleToken(cred, origin))
}

// A 15-minute token must not land in the credentials block, where a later job
// without the OIDC setup would be served it long after it expired.
func TestResolvePinned_OIDCGoesToTheAnalyticsBlockOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	target := &fakeStore{backend: auth.BackendFile}
	r := pinResolver(target)
	r.OIDC = oidcReturning(nil)

	_, origin, err := r.ResolvePinned(context.Background(), oidcEnvs(), true)

	require.NoError(t, err)
	assert.Equal(t, auth.ProvenanceOIDC, origin.Provenance)
	assert.Nil(t, target.saved, "the exchanged token was persisted as a durable credential")

	cfg, err := multiplatformconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	require.NoError(t, err)
	assert.Equal(t, "oidc-wat", cfg.AuthConfig.AuthToken)
	assert.Equal(t, auth.Origin{Backend: auth.BackendEnv, Provenance: auth.ProvenanceOIDC}, cfg.AuthConfig.Origin())
}

func TestOIDCOriginIsDistinctAndNotRefreshable(t *testing.T) {
	oidc := auth.Origin{Backend: auth.BackendEnv, Provenance: auth.ProvenanceOIDC}
	env := auth.Origin{Backend: auth.BackendEnv, Provenance: auth.ProvenanceInjected}

	assert.NotEqual(t, env.Label(), oidc.Label())
	assert.Equal(t, "oidc", oidc.ShortLabel())
	assert.False(t, oidc.StoreManaged())
	assert.False(t, auth.Origin{Backend: auth.BackendFile, Provenance: auth.ProvenanceOIDC}.StoreManaged(),
		"an OIDC token read back from a file must still never reach the store refresh path")
}

// `auth token` runs without a logger, so the warning is invisible there. When the
// exchange the user asked for is the only thing that could have produced a
// credential, its failure has to be the error.
func TestResolve_OIDCFailureIsTheErrorWhenNothingElseResolves(t *testing.T) {
	rejected := errors.New("bitrise rejected the token: sub=\"repo:acme@1/app@2:ref:refs/heads/main\"")
	r := &Resolver{
		OIDC:           func(context.Context, map[string]string) (auth.Credential, error) { return auth.Credential{}, rejected },
		Backends:       []store.Store{},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
	}

	_, _, err := r.Resolve(context.Background(), oidcEnvs())

	require.ErrorIs(t, err, rejected)
}

func TestResolve_MissingIDTokenPermissionIsTheErrorWhenNothingElseResolves(t *testing.T) {
	envs := oidcEnvs()
	delete(envs, auth.EnvGitHubOIDCRequestURL)
	delete(envs, auth.EnvGitHubOIDCRequestToken)
	r := &Resolver{
		Backends:       []store.Store{},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
	}

	_, _, err := r.Resolve(context.Background(), envs)

	require.ErrorIs(t, err, auth.ErrNoGitHubOIDCToken)
}

// Without a trust policy nothing changes: the usual error still names the env vars.
func TestResolve_NoPolicyKeepsTheUsualError(t *testing.T) {
	r := &Resolver{
		Backends:       []store.Store{},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
	}

	_, _, err := r.Resolve(context.Background(), map[string]string{})

	require.ErrorIs(t, err, auth.ErrTokenNotProvided)
}
