//go:build unit

package interactive

import (
	"context"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/keychain"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/oauth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/tui"
)

func silentLogger() log.Logger {
	return log.NewLogger(log.WithOutput(&strings.Builder{}))
}

// newResolver keeps the resolution off the real machine: the production
// ResolveAuthConfig also reads the keychain and on-disk config.
func newResolver(kc keychainStore, envs map[string]string, prompt string) wizardAuthResolver {
	return wizardAuthResolver{
		Logger:   silentLogger(),
		Keychain: kc,
		Envs:     envs,
		Prompt:   strings.NewReader(prompt),
		ResolveAuth: func(e map[string]string) (configcommon.CacheAuthConfig, configcommon.AuthSource, error) {
			if e[configcommon.EnvAuthToken] != "" && e[configcommon.EnvWorkspaceID] != "" {
				return configcommon.CacheAuthConfig{
					AuthToken:   e[configcommon.EnvAuthToken],
					WorkspaceID: e[configcommon.EnvWorkspaceID],
				}, configcommon.AuthSourceEnvVars, nil
			}

			return configcommon.CacheAuthConfig{}, configcommon.AuthSourceNone, assert.AnError
		},
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
	assert.Equal(t, "pat-new", auth.Config.AuthToken)
	assert.Equal(t, "ws-new", auth.Config.WorkspaceID)
	assert.Equal(t, configcommon.AuthSourceKeychain, auth.Source)
}

func TestResolveWizardAuth_EnvVarsSkipSignIn(t *testing.T) {
	r := newResolver(&stubKeychain{}, map[string]string{
		configcommon.EnvAuthToken:   "env-tok",
		configcommon.EnvWorkspaceID: "env-ws",
	}, "")
	r.Login = func(context.Context) (authpkg.TokenSet, error) {
		t.Fatal("must not sign in when the env vars are set")

		return authpkg.TokenSet{}, nil
	}

	auth := r.Resolve(context.Background())

	assert.Equal(t, configcommon.AuthSourceEnvVars, auth.Source)
	assert.Equal(t, "env-tok", auth.Config.AuthToken)
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

	assert.Equal(t, "fresh-pat", auth.Config.AuthToken)
	assert.Equal(t, configcommon.AuthSourceKeychain, auth.Source)
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
	assert.Equal(t, "pat-new", auth.Config.AuthToken)
	assert.Equal(t, "ws-2", auth.Config.WorkspaceID)
}

func TestResolveWizardAuth_ManualKeychainCredentialUsedAsIs(t *testing.T) {
	kc := &stubKeychain{creds: authpkg.TokenSet{AuthToken: "manual-pat", WorkspaceID: "ws-1"}}

	r := newResolver(kc, map[string]string{}, "")
	r.EnsureFresh = func(context.Context) (authpkg.TokenSet, error) {
		t.Fatal("a non-OAuth credential has nothing to refresh")

		return authpkg.TokenSet{}, nil
	}

	auth := r.Resolve(context.Background())

	assert.Equal(t, "manual-pat", auth.Config.AuthToken)
	assert.Equal(t, configcommon.AuthSourceKeychain, auth.Source)
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
	kc := &stubKeychain{creds: authpkg.TokenSet{AuthToken: "t", WorkspaceID: "ws-1"}}

	note := resolvedAuthNote(wizardAuth{
		Config: configcommon.CacheAuthConfig{AuthToken: "t", WorkspaceID: "ws-1"},
		Source: configcommon.AuthSourceKeychain,
	}, kc)
	assert.Contains(t, note, "Signing in was not needed")
	assert.Contains(t, note, "keychain")
	assert.Contains(t, note, "ws-1", "the note should name the workspace being used")

	envNote := resolvedAuthNote(wizardAuth{
		Config: configcommon.CacheAuthConfig{AuthToken: "t", WorkspaceID: "ws-2"},
		Source: configcommon.AuthSourceEnvVars,
	}, kc)
	assert.Contains(t, envNote, "environment variables")

	assert.Empty(t, resolvedAuthNote(wizardAuth{Source: configcommon.AuthSourceNone}, kc),
		"nothing to report when the token prompt is about to ask")
	assert.Empty(t, resolvedAuthNote(wizardAuth{
		Source:      configcommon.AuthSourceKeychain,
		SignedInNow: true,
	}, kc), "no note after a sign-in the user just performed")
}

func TestSelectChrome_GrowsWithTheDescription(t *testing.T) {
	assert.Equal(t, 2, tui.Chrome("one line"))
	assert.Equal(t, 4, tui.Chrome("one line\n\nplus a note"))
}

type failingStore struct {
	backend   authpkg.Backend
	saveErr   error
	saved     bool
	savedCred authpkg.TokenSet
}

func (f *failingStore) Backend() authpkg.Backend { return f.backend }

func (f *failingStore) Load() (authpkg.TokenSet, error) {
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
