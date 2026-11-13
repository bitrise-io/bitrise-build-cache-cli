//go:build unit

package live

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func pinResolver(target *fakeStore) *Resolver {
	return &Resolver{
		Backends:       []store.Store{target},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
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

// A keychain that cannot be read must not turn into a bare-token write to the
// config file: the record has to be merged against whichever backend is actually
// written, or an outage costs the user their refresh token.
func TestResolvePinned_FallbackMergesAgainstTheFileNotTheDeadKeychain(t *testing.T) {
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())

	fileLogin := auth.TokenSet{
		AuthToken: "old", WorkspaceID: "ws", RefreshToken: "refresh-me", Username: "dev",
	}
	require.NoError(t, store.NewFile().Save(fileLogin))

	deadKeychain := &fakeStore{backend: auth.BackendKeychain, loadErr: errors.New("no keyring"), saveErr: errors.New("no keyring")}
	r := &Resolver{
		Backends:       []store.Store{deadKeychain},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
	}

	_, _, err := r.ResolvePinned(t.Context(), envVars(), false)
	require.NoError(t, err)

	after, ok := multiplatformconfig.ReadCredentials(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	require.True(t, ok, "the fallback must have written the config file")
	assert.Equal(t, envToken, after.AuthToken)
	assert.Equal(t, "refresh-me", after.RefreshToken, "the file's refresh token must survive a keychain outage")
	assert.Equal(t, "dev", after.Username)
}

// Guards what the fallback test cannot: on the happy path the file store must be
// untouched. Switching to store.SaveExclusiveWithFallback would clear it, taking a
// login stored there with it, and every other test here would still pass.
func TestResolvePinned_HealthyKeychainLeavesTheFileUntouched(t *testing.T) {
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())

	fileLogin := auth.TokenSet{
		AuthToken:    "file-pat",
		WorkspaceID:  "file-ws",
		RefreshToken: "file-refresh",
		Username:     "dev",
	}
	require.NoError(t, store.NewFile().Save(fileLogin))

	// Backends[0] is the pin target; the env credential outranks the file, so the
	// pin actually runs.
	r := &Resolver{
		Backends:       []store.Store{store.NewKeychain(), store.NewFile()},
		AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false },
	}

	_, origin, err := r.ResolvePinned(t.Context(), envVars(), false)
	require.NoError(t, err)
	require.Equal(t, auth.BackendEnv, origin.Backend, "the env credential must be the one being pinned")

	kc, err := store.NewKeychain().Load()
	require.NoError(t, err, "the keychain is the target and must have been written")
	assert.Equal(t, envToken, kc.AuthToken)

	onDisk, ok := multiplatformconfig.ReadCredentials(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	require.True(t, ok, "the file store must still hold its record — an incidental write must not clear it")
	assert.Equal(t, fileLogin, onDisk, "the file store must be byte-for-byte untouched")
}

// Processes started outside a login shell (Xcode.app, the ccache helper) never see the
// VM env, so the brokered token is pinned off CI as well.
func TestResolvePinned_MirrorsABrokeredTokenOnAndOffCI(t *testing.T) {
	for _, isCI := range []bool{true, false} {
		t.Setenv("HOME", t.TempDir())
		r := &Resolver{Broker: brokerReturning(t, "org-slug", nil), Backends: []store.Store{}}

		_, origin, err := r.ResolvePinned(t.Context(), brokerEnvs(), isCI)
		require.NoError(t, err)
		require.Equal(t, auth.ProvenanceBrokered, origin.Provenance)

		cfg, _ := multiplatformconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
		assert.True(t, cfg.AuthConfig.Populated(), "isCI: %v", isCI)
	}
}

func jwtWithExp(t *testing.T, exp time.Time) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"exp": exp.Unix()})
	require.NoError(t, err)

	return "hdr." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

// The analytics block outlives the brokered token it holds, so an offline read must
// not serve it past its exp claim; a token it cannot read an expiry from is served.
func TestResolveNoRefresh_SkipsAnExpiredAnalyticsJWT(t *testing.T) {
	brokered := auth.Origin{Backend: auth.BackendJWT, Provenance: auth.ProvenanceBrokered}
	for _, tc := range []struct {
		name   string
		token  string
		origin auth.Origin
		served bool
	}{
		{"expired brokered JWT", jwtWithExp(t, time.Now().Add(-time.Minute)), brokered, false},
		{"unexpired brokered JWT", jwtWithExp(t, time.Now().Add(time.Hour)), brokered, true},
		{"static token", "static-pat", auth.Origin{Backend: auth.BackendFile, Provenance: auth.ProvenanceStatic}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			require.NoError(t, writeAnalyticsCredential(auth.Credential{Token: tc.token, WorkspaceID: "ws"}, tc.origin))
			r := &Resolver{Backends: []store.Store{}}

			cred, _, err := r.ResolveNoRefresh(map[string]string{})

			if !tc.served {
				require.ErrorIs(t, err, auth.ErrTokenNotProvided)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.token, cred.Token)
		})
	}
}
