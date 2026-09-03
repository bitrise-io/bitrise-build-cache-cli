//go:build unit

package live

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

// fakeStore is an in-memory backend, so the precedence table never touches the
// real keychain or the real config file.
type fakeStore struct {
	backend  auth.Backend
	ts       auth.TokenSet
	present  bool
	loadErr  error
	saved    *auth.TokenSet
	saveErr  error
	cleared  bool
	loadHits int
}

func (f *fakeStore) Backend() auth.Backend { return f.backend }

func (f *fakeStore) Load() (auth.TokenSet, error) {
	f.loadHits++
	if f.loadErr != nil {
		return auth.TokenSet{}, f.loadErr
	}
	if !f.present {
		return auth.TokenSet{}, store.ErrNotFound
	}

	return f.ts, nil
}

func (f *fakeStore) Save(c auth.TokenSet) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved, f.ts, f.present = &c, c, true

	return nil
}

func (f *fakeStore) Clear() error { f.cleared = true; return nil }

func umaJWT(t *testing.T, workspaceID string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"authorization": map[string]any{
			"permissions": []map[string]any{
				{"rsname": "default", "claims": map[string]any{"org_id": []string{workspaceID}}},
			},
		},
	})
	require.NoError(t, err)

	return "hdr." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

const (
	envToken = "env-tok"
	envWS    = "env-ws"
)

func envVars() map[string]string {
	return map[string]string{auth.EnvAuthToken: envToken, auth.EnvWorkspaceID: envWS}
}

func keychainTS() auth.TokenSet {
	return auth.TokenSet{AuthToken: "kc-tok", WorkspaceID: "kc-ws"}
}

func fileTS() auth.TokenSet {
	return auth.TokenSet{AuthToken: "file-tok", WorkspaceID: "file-ws"}
}

// TestResolve_Precedence pins the full order across every combination of which
// sources are present. It is the regression net for the whole facade: if a future
// change reorders resolution, this fails before any consumer notices.
func TestResolve_Precedence(t *testing.T) {
	jwt := umaJWT(t, "jwt-ws")

	cases := []struct {
		name                             string
		env, useJWT, keychain, file, leg bool
		wantToken, wantWS                string
		wantBackend                      auth.Backend
		wantProvenance                   auth.Provenance
	}{
		{name: "nothing", wantBackend: auth.BackendNone},
		{name: "env only", env: true, wantToken: envToken, wantWS: envWS, wantBackend: auth.BackendEnv, wantProvenance: auth.ProvenanceInjected},
		{name: "jwt only", useJWT: true, wantToken: jwt, wantWS: "jwt-ws", wantBackend: auth.BackendJWT, wantProvenance: auth.ProvenanceInjected},
		{name: "keychain only", keychain: true, wantToken: "kc-tok", wantWS: "kc-ws", wantBackend: auth.BackendKeychain, wantProvenance: auth.ProvenanceManual},
		{name: "file only", file: true, wantToken: "file-tok", wantWS: "file-ws", wantBackend: auth.BackendFile, wantProvenance: auth.ProvenanceManual},
		{name: "legacy only", leg: true, wantToken: "legacy-tok", wantWS: "legacy-ws", wantBackend: auth.BackendFile, wantProvenance: auth.ProvenanceStatic},

		{name: "env beats jwt", env: true, useJWT: true, wantToken: envToken, wantWS: envWS, wantBackend: auth.BackendEnv, wantProvenance: auth.ProvenanceInjected},
		{name: "env beats keychain", env: true, keychain: true, wantToken: envToken, wantWS: envWS, wantBackend: auth.BackendEnv, wantProvenance: auth.ProvenanceInjected},
		{name: "env beats file", env: true, file: true, wantToken: envToken, wantWS: envWS, wantBackend: auth.BackendEnv, wantProvenance: auth.ProvenanceInjected},
		{name: "env beats legacy", env: true, leg: true, wantToken: envToken, wantWS: envWS, wantBackend: auth.BackendEnv, wantProvenance: auth.ProvenanceInjected},
		{name: "jwt beats keychain", useJWT: true, keychain: true, wantToken: jwt, wantWS: "jwt-ws", wantBackend: auth.BackendJWT, wantProvenance: auth.ProvenanceInjected},
		{name: "jwt beats file", useJWT: true, file: true, wantToken: jwt, wantWS: "jwt-ws", wantBackend: auth.BackendJWT, wantProvenance: auth.ProvenanceInjected},
		{name: "jwt beats legacy", useJWT: true, leg: true, wantToken: jwt, wantWS: "jwt-ws", wantBackend: auth.BackendJWT, wantProvenance: auth.ProvenanceInjected},
		{name: "keychain beats file", keychain: true, file: true, wantToken: "kc-tok", wantWS: "kc-ws", wantBackend: auth.BackendKeychain, wantProvenance: auth.ProvenanceManual},
		{name: "keychain beats legacy", keychain: true, leg: true, wantToken: "kc-tok", wantWS: "kc-ws", wantBackend: auth.BackendKeychain, wantProvenance: auth.ProvenanceManual},
		{name: "file beats legacy", file: true, leg: true, wantToken: "file-tok", wantWS: "file-ws", wantBackend: auth.BackendFile, wantProvenance: auth.ProvenanceManual},

		{name: "all present", env: true, useJWT: true, keychain: true, file: true, leg: true, wantToken: envToken, wantWS: envWS, wantBackend: auth.BackendEnv, wantProvenance: auth.ProvenanceInjected},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			envs := map[string]string{}
			if tc.env {
				envs = envVars()
			}
			if tc.useJWT {
				envs[auth.EnvJWT] = jwt
			}

			r := &Resolver{
				Backends: []store.Store{
					&fakeStore{backend: auth.BackendKeychain, ts: keychainTS(), present: tc.keychain},
					&fakeStore{backend: auth.BackendFile, ts: fileTS(), present: tc.file},
				},
				AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) {
					return auth.Credential{Token: "legacy-tok", WorkspaceID: "legacy-ws"},
						auth.Origin{Backend: auth.BackendFile, Provenance: auth.ProvenanceStatic}, tc.leg
				},
			}

			cred, origin, err := r.ResolveNoRefresh(envs)

			if tc.wantBackend == auth.BackendNone {
				require.Error(t, err)
				assert.Equal(t, auth.BackendNone, origin.Backend)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantToken, cred.Token)
			assert.Equal(t, tc.wantWS, cred.WorkspaceID)
			assert.Equal(t, tc.wantBackend, origin.Backend)
			assert.Equal(t, tc.wantProvenance, origin.Provenance)
		})
	}
}

// PreferStored is the wizard's order: a stale token exported by a shell rc file
// must not shadow the login on the machine in front of the user.
func TestResolve_PreferStored_storeBeatsEnv(t *testing.T) {
	r := &Resolver{
		Prefer:         PreferStored,
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: keychainTS(), present: true}},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
	}

	cred, origin, err := r.ResolveNoRefresh(envVars())

	require.NoError(t, err)
	assert.Equal(t, "kc-tok", cred.Token)
	assert.Equal(t, auth.BackendKeychain, origin.Backend)
}

func TestResolve_PreferStored_fallsBackToEnvWhenNothingStored(t *testing.T) {
	r := &Resolver{
		Prefer:         PreferStored,
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain}},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
	}

	cred, origin, err := r.ResolveNoRefresh(envVars())

	require.NoError(t, err)
	assert.Equal(t, envToken, cred.Token)
	assert.Equal(t, auth.BackendEnv, origin.Backend)
}

// Both store backends can hold an OAuth login, so both must refresh. Gating on the
// keychain alone is what left file-stored logins serving an expired PAT forever.
func TestResolve_RefreshesEveryStoreBackend(t *testing.T) {
	for _, backend := range []auth.Backend{auth.BackendKeychain, auth.BackendFile} {
		t.Run(backend.String(), func(t *testing.T) {
			stale := auth.TokenSet{AuthToken: "stale", WorkspaceID: "ws", RefreshToken: "rt", PATExpiry: time.Now().Add(-time.Hour)}
			fresh := auth.TokenSet{AuthToken: "fresh", WorkspaceID: "ws", RefreshToken: "rt2", PATExpiry: time.Now().Add(time.Hour)}

			refreshed := false
			r := &Resolver{
				Backends:       []store.Store{&fakeStore{backend: backend, ts: stale, present: true}},
				AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
				Refresh: func(_ context.Context, _ auth.TokenSet, _ store.Store) (auth.TokenSet, error) {
					refreshed = true

					return fresh, nil
				},
			}

			cred, origin, err := r.Resolve(t.Context(), map[string]string{})

			require.NoError(t, err)
			assert.True(t, refreshed, "a store-managed credential must be refreshed")
			assert.Equal(t, "fresh", cred.Token)
			assert.Equal(t, fresh.PATExpiry, cred.Expiry, "the refreshed expiry must reach the caller")
			assert.Equal(t, backend, origin.Backend)
		})
	}
}

func TestResolve_InjectedCredentialsAreNeverRefreshed(t *testing.T) {
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain}},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
		Refresh: func(context.Context, auth.TokenSet, store.Store) (auth.TokenSet, error) {
			t.Fatal("env credentials carry no refresh token; refresh must not be attempted")

			return auth.TokenSet{}, nil
		},
	}

	_, origin, err := r.Resolve(t.Context(), envVars())

	require.NoError(t, err)
	assert.Equal(t, auth.BackendEnv, origin.Backend)
}

// A failed refresh is a degraded state, not a fatal one: a slightly stale token
// still authenticates far more often than it doesn't.
func TestResolve_ServesTheStoredCredentialWhenRefreshFails(t *testing.T) {
	stored := auth.TokenSet{AuthToken: "stored", WorkspaceID: "ws", RefreshToken: "rt"}
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: stored, present: true}},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
		Refresh: func(context.Context, auth.TokenSet, store.Store) (auth.TokenSet, error) {
			return auth.TokenSet{}, errors.New("network down")
		},
	}

	cred, origin, err := r.Resolve(t.Context(), map[string]string{})

	require.NoError(t, err)
	assert.Equal(t, "stored", cred.Token)
	assert.Equal(t, auth.BackendKeychain, origin.Backend)
}

func TestResolveNoRefresh_neverRefreshes(t *testing.T) {
	stored := auth.TokenSet{AuthToken: "stored", WorkspaceID: "ws", RefreshToken: "rt", PATExpiry: time.Now().Add(-time.Hour)}
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: stored, present: true}},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
		Refresh: func(context.Context, auth.TokenSet, store.Store) (auth.TokenSet, error) {
			t.Fatal("ResolveNoRefresh must not refresh")

			return auth.TokenSet{}, nil
		},
	}

	cred, _, err := r.ResolveNoRefresh(map[string]string{})

	require.NoError(t, err)
	assert.Equal(t, "stored", cred.Token)
	assert.True(t, cred.Expired())
}

func TestResolve_MalformedJWTIsReportedNotSwallowed(t *testing.T) {
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain}},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
	}

	_, _, err := r.ResolveNoRefresh(map[string]string{auth.EnvJWT: "not-a-jwt"})

	require.Error(t, err)
}

func TestResolve_MissingWorkspaceIDIsDistinctFromMissingToken(t *testing.T) {
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain}},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
	}

	_, _, err := r.ResolveNoRefresh(map[string]string{auth.EnvAuthToken: envToken})
	require.ErrorIs(t, err, auth.ErrWorkspaceIDNotProvided)

	_, _, err = r.ResolveNoRefresh(map[string]string{auth.EnvWorkspaceID: envWS})
	require.ErrorIs(t, err, auth.ErrTokenNotProvided)
}

// A backend that errors (a locked keychain, say) must not hide a credential in the
// next one down.
func TestResolve_UnreadableBackendFallsThrough(t *testing.T) {
	r := &Resolver{
		Backends: []store.Store{
			&fakeStore{backend: auth.BackendKeychain, loadErr: errors.New("keychain is locked")},
			&fakeStore{backend: auth.BackendFile, ts: fileTS(), present: true},
		},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
	}

	cred, origin, err := r.ResolveNoRefresh(map[string]string{})

	require.NoError(t, err)
	assert.Equal(t, "file-tok", cred.Token)
	assert.Equal(t, auth.BackendFile, origin.Backend)
}

// A half-written record is not a credential.
func TestResolve_PartialRecordIsSkipped(t *testing.T) {
	r := &Resolver{
		Backends: []store.Store{
			&fakeStore{backend: auth.BackendKeychain, ts: auth.TokenSet{AuthToken: "tok-no-ws"}, present: true},
			&fakeStore{backend: auth.BackendFile, ts: fileTS(), present: true},
		},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
	}

	cred, _, err := r.ResolveNoRefresh(map[string]string{})

	require.NoError(t, err)
	assert.Equal(t, "file-tok", cred.Token)
}

func TestBoundGet_returnsAZeroCredentialRatherThanPanicking(t *testing.T) {
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain}},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
	}

	assert.Equal(t, auth.Credential{}, r.Bind(map[string]string{}).Get(t.Context()))
}

// A manual `auth set` token has no refresh token. Attempting the flow yields
// ErrNotLoggedIn, which a FailFast caller would misread as a dead login.
func TestResolve_ManualStoredCredentialIsNotRefreshed(t *testing.T) {
	manual := auth.TokenSet{AuthToken: "manual", WorkspaceID: "ws"}
	r := &Resolver{
		OnRefreshFailure: FailFast,
		Backends:         []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: manual, present: true}},
		AnalyticsBlock:   func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
		Refresh: func(context.Context, auth.TokenSet, store.Store) (auth.TokenSet, error) {
			t.Fatal("a credential with no refresh token must not enter the refresh flow")

			return auth.TokenSet{}, nil
		},
	}

	cred, origin, err := r.Resolve(t.Context(), map[string]string{})

	require.NoError(t, err)
	assert.Equal(t, "manual", cred.Token)
	assert.Equal(t, auth.ProvenanceManual, origin.Provenance)
}

// FailFast is for interactive callers: a dead refresh token must surface, not be
// papered over with a token the backend will reject.
func TestResolve_FailFastReportsARefreshFailure(t *testing.T) {
	login := auth.TokenSet{AuthToken: "stale", WorkspaceID: "ws", RefreshToken: "revoked"}
	r := &Resolver{
		OnRefreshFailure: FailFast,
		Backends:         []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: login, present: true}},
		AnalyticsBlock:   func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
		Refresh: func(context.Context, auth.TokenSet, store.Store) (auth.TokenSet, error) {
			return auth.TokenSet{}, errors.New("refresh token revoked")
		},
	}

	_, _, err := r.Resolve(t.Context(), map[string]string{})

	require.Error(t, err)
}

// The legacy authConfig block predates refresh tokens; treating it as refreshable
// sends the Bazel helper into a flow that can only fail.
func TestResolve_LegacyBlockIsNotStoreManaged(t *testing.T) {
	r := &Resolver{
		Backends: []store.Store{&fakeStore{backend: auth.BackendKeychain}},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) {
			return auth.Credential{Token: "l", WorkspaceID: "w"},
				auth.Origin{Backend: auth.BackendFile, Provenance: auth.ProvenanceStatic}, true
		},
		Refresh: func(context.Context, auth.TokenSet, store.Store) (auth.TokenSet, error) {
			t.Fatal("the legacy block is not store-managed")

			return auth.TokenSet{}, nil
		},
	}

	_, origin, err := r.Resolve(t.Context(), map[string]string{})

	require.NoError(t, err)
	assert.False(t, origin.StoreManaged())
	assert.Equal(t, auth.ProvenanceStatic, origin.Provenance)
}

// A manual token in an earlier backend must not hide a login in a later one, or
// the login is never refreshed.
func TestResolve_PrefersTheOAuthManagedRecordAcrossBackends(t *testing.T) {
	manual := auth.TokenSet{AuthToken: "manual", WorkspaceID: "ws"}
	login := auth.TokenSet{AuthToken: "login", WorkspaceID: "ws", RefreshToken: "rt"}

	refreshed := false
	r := &Resolver{
		Backends: []store.Store{
			&fakeStore{backend: auth.BackendKeychain, ts: manual, present: true},
			&fakeStore{backend: auth.BackendFile, ts: login, present: true},
		},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
		Refresh: func(_ context.Context, ts auth.TokenSet, _ store.Store) (auth.TokenSet, error) {
			refreshed = true
			assert.Equal(t, "login", ts.AuthToken)

			return ts, nil
		},
	}

	cred, origin, err := r.Resolve(t.Context(), map[string]string{})

	require.NoError(t, err)
	assert.Equal(t, "login", cred.Token)
	assert.Equal(t, auth.BackendFile, origin.Backend)
	assert.True(t, refreshed, "the OAuth-managed record is the one that must be refreshed")
}

// The legacy block records whether its token is a CI JWT, and GradleToken needs
// that: a JWT is sent as-is, a PAT is prefixed with the workspace.
func TestResolve_LegacyJWTKeepsItsOrigin(t *testing.T) {
	r := &Resolver{
		Backends: []store.Store{&fakeStore{backend: auth.BackendKeychain}},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) {
			return auth.Credential{Token: "jwt-tok", WorkspaceID: "ws"},
				auth.Origin{Backend: auth.BackendJWT, Provenance: auth.ProvenanceInjected}, true
		},
	}

	cred, origin, err := r.ResolveNoRefresh(map[string]string{})

	require.NoError(t, err)
	assert.Equal(t, "jwt-tok", auth.GradleToken(cred, origin), "a JWT must not be workspace-prefixed")
}
