//go:build unit

package interactive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/oauth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/tui"
)

func silentLogger() log.Logger {
	return log.NewLogger(log.WithOutput(&strings.Builder{}))
}

// newResolver keeps resolution off the real machine: the production resolver
// also reads the OS keychain and the on-disk config.
func newResolver(kc store.Store, envs map[string]string, prompt string) wizardAuthResolver {
	res := live.Default(silentLogger())
	res.Prefer = live.PreferStored
	res.Backends = []store.Store{kc}
	res.AnalyticsBlock = func() (authpkg.Credential, authpkg.Origin, bool) {
		return authpkg.Credential{}, authpkg.Origin{}, false
	}

	return wizardAuthResolver{
		Logger:   silentLogger(),
		Store:    kc,
		Envs:     envs,
		Prompt:   strings.NewReader(prompt),
		Resolver: res,
	}
}

func TestResolveWizardAuth_NoCredentialsSignsIn(t *testing.T) {
	loginCalls := 0
	r := newResolver(&stubKeychain{}, map[string]string{}, "\n")
	r.Login = func(context.Context) (authpkg.TokenSet, error) {
		loginCalls++

		return authpkg.TokenSet{AuthToken: "pat-new", WorkspaceID: "ws-new", RefreshToken: "r"}, nil
	}

	auth := r.Resolve(context.Background())

	assert.Equal(t, 1, loginCalls)
	assert.True(t, auth.SignedInNow)
	assert.False(t, auth.NeedsManualPrompt())
	assert.Equal(t, "pat-new", auth.Config.Token)
	assert.Equal(t, "ws-new", auth.Config.WorkspaceID)
	assert.Equal(t, authpkg.BackendKeychain, auth.Origin.Backend)
}

func TestResolveWizardAuth_EnvVarsSkipSignIn(t *testing.T) {
	r := newResolver(&stubKeychain{}, map[string]string{
		authpkg.EnvAuthToken:   "env-tok",
		authpkg.EnvWorkspaceID: "env-ws",
	}, "")
	r.Login = func(context.Context) (authpkg.TokenSet, error) {
		t.Fatal("must not sign in when the env vars are set")

		return authpkg.TokenSet{}, nil
	}

	auth := r.Resolve(context.Background())

	assert.Equal(t, authpkg.BackendEnv, auth.Origin.Backend)
	assert.Equal(t, "env-tok", auth.Config.Token)
	assert.False(t, auth.SignedInNow)
}

// Accessible mode (TERM=dumb) hands stdin to huh, so there's nothing to confirm
// a sign-in on. Launching a browser there would block on a callback that never
// arrives — the manual token prompt has to win instead.
func TestResolveWizardAuth_NoPromptStreamSkipsSignIn(t *testing.T) {
	r := newResolver(&stubKeychain{}, map[string]string{}, "")
	r.Prompt = nil
	r.Login = func(context.Context) (authpkg.TokenSet, error) {
		t.Fatal("must not open a browser when the sign-in can't be confirmed")

		return authpkg.TokenSet{}, nil
	}

	auth := r.Resolve(context.Background())

	assert.True(t, auth.NeedsManualPrompt())
	assert.False(t, auth.SignedInNow)
}

func TestResolveWizardAuth_DeclinedSignInFallsBackToManualPrompt(t *testing.T) {
	r := newResolver(&stubKeychain{}, map[string]string{}, "s\n")
	r.Login = func(context.Context) (authpkg.TokenSet, error) {
		t.Fatal("must not sign in after the user skipped")

		return authpkg.TokenSet{}, nil
	}

	auth := r.Resolve(context.Background())

	assert.True(t, auth.NeedsManualPrompt())
	assert.False(t, auth.SignedInNow)
}

func TestResolveWizardAuth_FailedSignInFallsBackToManualPrompt(t *testing.T) {
	r := newResolver(&stubKeychain{}, map[string]string{}, "\n")
	r.Login = func(context.Context) (authpkg.TokenSet, error) {
		return authpkg.TokenSet{}, assert.AnError
	}

	auth := r.Resolve(context.Background())

	assert.True(t, auth.NeedsManualPrompt(), "a failed browser login must not dead-end the wizard")
}

func TestResolveWizardAuth_RefreshesStoredOAuthLogin(t *testing.T) {
	kc := &stubKeychain{creds: authpkg.TokenSet{
		AuthToken:    "stale-pat",
		WorkspaceID:  "ws-1",
		RefreshToken: "refresh-1",
	}}

	r := newResolver(kc, map[string]string{}, "")
	r.EnsureFresh = func(context.Context) (authpkg.TokenSet, error) {
		return authpkg.TokenSet{AuthToken: "fresh-pat", WorkspaceID: "ws-1", RefreshToken: "refresh-1"}, nil
	}
	r.Login = func(context.Context) (authpkg.TokenSet, error) {
		t.Fatal("a refreshable login must not trigger a new browser sign-in")

		return authpkg.TokenSet{}, nil
	}

	auth := r.Resolve(context.Background())

	assert.Equal(t, "fresh-pat", auth.Config.Token)
	assert.Equal(t, authpkg.BackendKeychain, auth.Origin.Backend)
	assert.False(t, auth.SignedInNow)
}

func TestResolveWizardAuth_UnrefreshableLoginSignsInAgain(t *testing.T) {
	kc := &stubKeychain{creds: authpkg.TokenSet{
		AuthToken:    "stale-pat",
		WorkspaceID:  "ws-1",
		RefreshToken: "revoked",
	}}

	r := newResolver(kc, map[string]string{}, "\n")
	r.EnsureFresh = func(context.Context) (authpkg.TokenSet, error) {
		return authpkg.TokenSet{}, oauth.ErrLoginRequired
	}
	r.Login = func(context.Context) (authpkg.TokenSet, error) {
		return authpkg.TokenSet{AuthToken: "pat-new", WorkspaceID: "ws-2", RefreshToken: "r"}, nil
	}

	auth := r.Resolve(context.Background())

	assert.True(t, auth.SignedInNow)
	assert.Equal(t, "pat-new", auth.Config.Token)
	assert.Equal(t, "ws-2", auth.Config.WorkspaceID)
}

// A wrapped ErrLoginRequired still means "sign in again" — the flow wraps it
// with the underlying HTTP failure, and errors.Is has to see through that.
func TestResolveWizardAuth_WrappedLoginRequiredStillSignsIn(t *testing.T) {
	kc := &stubKeychain{creds: authpkg.TokenSet{
		AuthToken:    "stale-pat",
		WorkspaceID:  "ws-1",
		RefreshToken: "revoked",
	}}

	loginCalls := 0
	r := newResolver(kc, map[string]string{}, "\n")
	r.EnsureFresh = func(context.Context) (authpkg.TokenSet, error) {
		return authpkg.TokenSet{}, fmt.Errorf("outer: %w", oauth.ErrLoginRequired)
	}
	r.Login = func(context.Context) (authpkg.TokenSet, error) {
		loginCalls++

		return authpkg.TokenSet{AuthToken: "pat-new", WorkspaceID: "ws-2", RefreshToken: "r"}, nil
	}

	auth := r.Resolve(context.Background())

	assert.Equal(t, 1, loginCalls)
	assert.True(t, auth.SignedInNow)
}

// A transient refresh error (network unreachable, cancelled ctx) is what turned
// this into a two-run wizard: forcing a browser sign-in there when the stored
// token is still live is exactly what ACI-5351 fixes.
func TestResolveWizardAuth_TransientRefreshErrorServesStoredAndDoesNotSignIn(t *testing.T) {
	kc := &stubKeychain{creds: authpkg.TokenSet{
		AuthToken:    "stored-pat",
		WorkspaceID:  "ws-1",
		RefreshToken: "refresh-1",
	}}

	r := newResolver(kc, map[string]string{}, "")
	r.EnsureFresh = func(context.Context) (authpkg.TokenSet, error) {
		return authpkg.TokenSet{}, errors.New("network unreachable")
	}
	r.Login = func(context.Context) (authpkg.TokenSet, error) {
		t.Fatal("a transient refresh error must not trigger a new browser sign-in")

		return authpkg.TokenSet{}, nil
	}

	auth := r.Resolve(context.Background())

	assert.False(t, auth.SignedInNow)
	assert.False(t, auth.NeedsManualPrompt())
	assert.Equal(t, "stored-pat", auth.Config.Token)
	assert.Equal(t, "ws-1", auth.Config.WorkspaceID)
	assert.Equal(t, authpkg.BackendKeychain, auth.Origin.Backend)
}

// A cancelled context on refresh is a transient network error dressed differently,
// so the stored credential still has to survive it.
func TestResolveWizardAuth_CtxCanceledDuringRefreshServesStored(t *testing.T) {
	kc := &stubKeychain{creds: authpkg.TokenSet{
		AuthToken:    "stored-pat",
		WorkspaceID:  "ws-1",
		RefreshToken: "refresh-1",
	}}

	r := newResolver(kc, map[string]string{}, "")
	r.EnsureFresh = func(context.Context) (authpkg.TokenSet, error) {
		return authpkg.TokenSet{}, context.Canceled
	}
	r.Login = func(context.Context) (authpkg.TokenSet, error) {
		t.Fatal("a cancelled refresh must not trigger a new browser sign-in")

		return authpkg.TokenSet{}, nil
	}

	auth := r.Resolve(context.Background())

	assert.False(t, auth.SignedInNow)
	assert.False(t, auth.NeedsManualPrompt())
	assert.Equal(t, "stored-pat", auth.Config.Token)
	assert.Equal(t, authpkg.BackendKeychain, auth.Origin.Backend)
}

func TestResolveWizardAuth_ManualKeychainCredentialUsedAsIs(t *testing.T) {
	kc := &stubKeychain{creds: authpkg.TokenSet{AuthToken: "manual-pat", WorkspaceID: "ws-1"}}

	r := newResolver(kc, map[string]string{}, "")
	r.EnsureFresh = func(context.Context) (authpkg.TokenSet, error) {
		t.Fatal("a non-OAuth credential has nothing to refresh")

		return authpkg.TokenSet{}, nil
	}

	auth := r.Resolve(context.Background())

	assert.Equal(t, "manual-pat", auth.Config.Token)
	assert.Equal(t, authpkg.BackendKeychain, auth.Origin.Backend)
}

// A fresh login rewrites the keychain, so the wizard must re-read it before it
// later persists a display name over the same record.
func TestResolveWizardAuth_ReReadsKeychainAfterSignIn(t *testing.T) {
	kc := &reloadingKeychain{after: authpkg.TokenSet{
		AuthToken:    "pat-new",
		WorkspaceID:  "ws-new",
		RefreshToken: "refresh-new",
		JWT:          "jwt-new",
	}}

	r := newResolver(kc, map[string]string{}, "\n")
	r.Login = func(context.Context) (authpkg.TokenSet, error) {
		kc.loggedIn = true

		return authpkg.TokenSet{AuthToken: "pat-new", WorkspaceID: "ws-new", RefreshToken: "refresh-new"}, nil
	}

	auth := r.Resolve(context.Background())

	require.True(t, auth.SignedInNow)
	assert.Equal(t, "refresh-new", auth.Stored.RefreshToken, "OAuth tokens must survive a later keychain write")
	assert.Equal(t, "jwt-new", auth.Stored.JWT)
}

type reloadingKeychain struct {
	after    authpkg.TokenSet
	loggedIn bool
	saved    authpkg.TokenSet
}

func (k *reloadingKeychain) Load() (authpkg.TokenSet, error) {
	if k.loggedIn {
		return k.after, nil
	}

	return authpkg.TokenSet{}, keychain.ErrNotFound
}

func (k *reloadingKeychain) Backend() authpkg.Backend { return authpkg.BackendKeychain }
func (k *reloadingKeychain) Clear() error             { return nil }

func (k *reloadingKeychain) Save(c authpkg.TokenSet) error {
	k.saved = c

	return nil
}

func TestConfirmWizardLogin(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  bool
	}{
		{input: "\n", want: true},
		{input: "  \n", want: true},
		{input: "yes\n", want: true},
		{input: "s\n", want: false},
		{input: "skip\n", want: false},
		{input: "N\n", want: false},
		{input: "", want: false}, // EOF (piped/closed stdin): don't open a browser
	} {
		assert.Equal(t, tc.want, confirmWizardLogin(silentLogger(), strings.NewReader(tc.input)), "input=%q", tc.input)
	}

	assert.False(t, confirmWizardLogin(silentLogger(), nil),
		"no stream to confirm on: must not open a browser and then block on a callback")
}

func TestResolvedAuthNote(t *testing.T) {
	note := resolvedAuthNote(wizardAuth{
		Config: authpkg.Credential{Token: "t", WorkspaceID: "ws-1"},
		Origin: authpkg.Origin{Backend: authpkg.BackendKeychain},
	})
	assert.Contains(t, note, "Signing in was not needed")
	assert.Contains(t, note, "keychain")
	assert.Contains(t, note, "ws-1", "the note should name the workspace being used")

	envNote := resolvedAuthNote(wizardAuth{
		Config: authpkg.Credential{Token: "t", WorkspaceID: "ws-2"},
		Origin: authpkg.Origin{Backend: authpkg.BackendEnv},
	})
	assert.Contains(t, envNote, "environment variables")

	assert.Empty(t, resolvedAuthNote(wizardAuth{}),
		"nothing to report when the token prompt is about to ask")
	assert.Empty(t, resolvedAuthNote(wizardAuth{
		Origin:      authpkg.Origin{Backend: authpkg.BackendKeychain},
		SignedInNow: true,
	}), "no note after a sign-in the user just performed")
}

func TestSelectChrome_GrowsWithTheDescription(t *testing.T) {
	assert.Equal(t, 2, tui.Chrome("one line"))
	assert.Equal(t, 4, tui.Chrome("one line\n\nplus a note"))
}

type failingStore struct {
	backend   authpkg.Backend
	saveErr   error
	loadErr   error
	saved     bool
	savedCred authpkg.TokenSet
}

func (f *failingStore) Backend() authpkg.Backend { return f.backend }

func (f *failingStore) Load() (authpkg.TokenSet, error) {
	if f.loadErr != nil {
		return authpkg.TokenSet{}, f.loadErr
	}
	if !f.saved {
		return authpkg.TokenSet{}, store.ErrNotFound
	}

	return f.savedCred, nil
}

func (f *failingStore) Save(c authpkg.TokenSet) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved, f.savedCred = true, c

	return nil
}

func (f *failingStore) Clear() error {
	f.saved, f.savedCred = false, authpkg.TokenSet{}

	return nil
}

// A locked keychain must not throw away a completed sign-in.
func TestSaveLoginWithFallback_FallsBackToTheConfigFile(t *testing.T) {
	target := &failingStore{backend: authpkg.BackendKeychain, saveErr: assert.AnError}
	creds := authpkg.TokenSet{AuthToken: "pat-1", WorkspaceID: "ws-1", RefreshToken: "refresh-1"}

	origin, err := saveLoginWithFallback(silentLogger(), target, "", creds)

	require.NoError(t, err)
	assert.Equal(t, authpkg.BackendFile, origin.Backend, "the fallback backend should be reported, not the one that failed")
}

// An explicit --storage choice is the caller's decision; don't silently move it.
func TestSaveLoginWithFallback_HonoursExplicitStorage(t *testing.T) {
	target := &failingStore{backend: authpkg.BackendKeychain, saveErr: assert.AnError}

	_, err := saveLoginWithFallback(silentLogger(), target, "keychain",
		authpkg.TokenSet{AuthToken: "pat-1", WorkspaceID: "ws-1", RefreshToken: "r"})

	require.Error(t, err)
}

func TestSaveLoginWithFallback_NoFallbackNeeded(t *testing.T) {
	target := &failingStore{backend: authpkg.BackendKeychain}
	creds := authpkg.TokenSet{AuthToken: "pat-1", WorkspaceID: "ws-1", RefreshToken: "refresh-1"}

	origin, err := saveLoginWithFallback(silentLogger(), target, "", creds)

	require.NoError(t, err)
	assert.Equal(t, authpkg.BackendKeychain, origin.Backend)
	assert.True(t, target.saved)
	assert.Equal(t, "refresh-1", target.savedCred.RefreshToken, "the refresh token must be persisted")
}

// A host with no keyring is a supported setup, not a fault: credentials live in
// the config file there. The wizard must treat it as empty quietly, the way
// keychain-smoke warns rather than errors.
func TestLoadStoredCredentials_KeyringlessHostIsQuiet(t *testing.T) {
	var out bytes.Buffer
	logger := log.NewLogger(log.WithOutput(&out))

	kc := &failingStore{backend: authpkg.BackendKeychain,
		loadErr: fmt.Errorf("%w: %w", store.ErrNotFound, keychain.ErrUnavailable)}

	got := loadStoredCredentials(logger, kc)

	assert.Equal(t, authpkg.TokenSet{}, got)
	assert.NotContains(t, out.String(), "Could not read", "a keyring-less host must not produce a warning")
}
