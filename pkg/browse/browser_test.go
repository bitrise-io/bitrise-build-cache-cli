//go:build unit

package browse

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/browse"
)

type fakeAuthResolver struct {
	cred           auth.Credential
	workspacesOnly bool
	err            error
	slugs          []string
}

func (f fakeAuthResolver) ResolveNoRefresh(_ map[string]string) (auth.Credential, auth.Origin, bool, error) {
	return f.cred, auth.Origin{}, f.workspacesOnly, f.err
}

func (f fakeAuthResolver) StoredWorkspaceSlugs() []string { return f.slugs }

// browse_ErrNoOpener returns the internal sentinel so the public-API
// test can drive the warn-path branch without importing the internal
// package in line.
func browse_ErrNoOpener() error { return browse.ErrNoOpener }

//nolint:gochecknoglobals
var errSimulatedExecFailure = errors.New("simulated exec failure")

func newLogger() log.Logger {
	return log.NewLogger(log.WithOutput(&bytes.Buffer{}))
}

// stubOpener records the URL it was asked to open and returns the
// pre-configured error (nil by default). Used to assert that Browser.Open
// hands off to the opener with the expected URL.
type stubOpener struct {
	seenURL string
	err     error
}

func (s *stubOpener) Open(_ context.Context, url string) error {
	s.seenURL = url

	return s.err
}

func TestBrowse_workspaceFromEnv_buildsListURLWithCIProviderUnknown(t *testing.T) {
	op := &stubOpener{}
	b := &Browser{Logger: newLogger(), Opener: op}

	got, err := b.Open(context.Background(), Params{
		Envs:    map[string]string{"BITRISE_BUILD_CACHE_WORKSPACE_ID": "ws_abc"},
		BaseURL: "https://app.bitrise.io",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://app.bitrise.io/build-cache/ws_abc/invocations?ci_provider=unknown", got.URL)
	assert.Equal(t, "ws_abc", got.WorkspaceID)
	assert.Empty(t, got.InvocationID)
	assert.Equal(t, got.URL, op.seenURL)
}

func TestBrowse_invocationID_deepLinks_noFilter(t *testing.T) {
	op := &stubOpener{}
	b := &Browser{Logger: newLogger(), Opener: op}

	got, err := b.Open(context.Background(), Params{
		WorkspaceID:  "ws_abc",
		InvocationID: "inv_xyz",
		BaseURL:      "https://app.bitrise.io",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://app.bitrise.io/build-cache/ws_abc/invocations/inv_xyz", got.URL)
	assert.Equal(t, "inv_xyz", got.InvocationID)
	assert.NotContains(t, got.URL, "ci_provider=")
}

func TestBrowse_authConfigFallback_resolvesWorkspaceID(t *testing.T) {
	op := &stubOpener{}
	b := &Browser{
		Logger: newLogger(),
		Opener: op,
		WorkspaceFromAuth: func(_ map[string]string) (string, error) {
			return "ws_from_auth", nil
		},
	}

	got, err := b.Open(context.Background(), Params{
		Envs:    map[string]string{},
		BaseURL: "https://app.bitrise.io",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://app.bitrise.io/build-cache/ws_from_auth/invocations?ci_provider=unknown", got.URL)
	assert.Equal(t, "ws_from_auth", got.WorkspaceID)
}

func TestBrowse_authConfigFallback_errorFallsThroughToSentinel(t *testing.T) {
	b := &Browser{
		Logger: newLogger(),
		Opener: &stubOpener{},
		WorkspaceFromAuth: func(_ map[string]string) (string, error) {
			return "", errors.New("no config on disk")
		},
	}

	_, err := b.Open(context.Background(), Params{Envs: map[string]string{}})
	require.ErrorIs(t, err, ErrWorkspaceNotConfigured)
}

func TestBrowse_printOnlySkipsOpener(t *testing.T) {
	op := &stubOpener{}
	b := &Browser{Logger: newLogger(), Opener: op}

	got, err := b.Open(context.Background(), Params{
		WorkspaceID: "ws_abc",
		PrintOnly:   true,
		BaseURL:     "https://app.bitrise.io",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, got.URL)
	assert.Empty(t, op.seenURL, "PrintOnly must not invoke the opener")
}

func TestBrowse_missingWorkspaceReturnsSentinel(t *testing.T) {
	b := &Browser{
		Logger: newLogger(),
		Opener: &stubOpener{},
		WorkspaceFromAuth: func(_ map[string]string) (string, error) {
			return "", nil
		},
	}

	_, err := b.Open(context.Background(), Params{Envs: map[string]string{}})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrWorkspaceNotConfigured)
}

func TestBrowse_openerErrNoOpener_emitsNoSupportedLauncherWarn(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(log.WithOutput(&buf))

	op := &stubOpener{err: browse_ErrNoOpener()}
	b := &Browser{Logger: logger, Opener: op}

	got, err := b.Open(context.Background(), Params{
		WorkspaceID: "ws_abc",
		BaseURL:     "https://app.bitrise.io",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, got.URL)
	assert.Contains(t, buf.String(), "No default browser launcher for this OS")
}

// A workspaces-only auth store with a single per-workspace entry and no marker
// upstack: the sole slug is the default so `browse` still resolves.
func TestWorkspaceFromAuth_workspacesOnlySingleSlugDefaults(t *testing.T) {
	t.Chdir(t.TempDir())

	resolver := fakeAuthResolver{workspacesOnly: true, slugs: []string{"acme"}}

	slug, err := workspaceFromAuth(resolver, map[string]string{})
	require.NoError(t, err)
	assert.Equal(t, "acme", slug)
}

// Multiple stored workspaces with no marker: the caller cannot pick, so we
// surface the ambiguity with an actionable enumeration.
func TestWorkspaceFromAuth_workspacesOnlyMultipleSlugsErrors(t *testing.T) {
	t.Chdir(t.TempDir())

	resolver := fakeAuthResolver{workspacesOnly: true, slugs: []string{"acme", "beta"}}

	_, err := workspaceFromAuth(resolver, map[string]string{})
	require.ErrorIs(t, err, ErrAmbiguousWorkspace)
	assert.Contains(t, err.Error(), "acme")
	assert.Contains(t, err.Error(), "beta")
}

// Nothing at all in a workspaces-only state is a soft signal so the caller can
// render `ErrWorkspaceNotConfigured` — never an internal error string.
func TestWorkspaceFromAuth_workspacesOnlyZeroSlugsSoftlyReturnsEmpty(t *testing.T) {
	t.Chdir(t.TempDir())

	resolver := fakeAuthResolver{workspacesOnly: true}

	slug, err := workspaceFromAuth(resolver, map[string]string{})
	require.NoError(t, err)
	assert.Empty(t, slug)
}

func TestBrowse_openerGenericError_emitsCouldNotAutoLaunchWarn(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogger(log.WithOutput(&buf))

	op := &stubOpener{err: errSimulatedExecFailure}
	b := &Browser{Logger: logger, Opener: op}

	got, err := b.Open(context.Background(), Params{
		WorkspaceID: "ws_abc",
		BaseURL:     "https://app.bitrise.io",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, got.URL)
	assert.Contains(t, buf.String(), "Could not auto-launch the browser")
	// Generic-error message should include the underlying error text so
	// post-mortem isn't blind.
	assert.Contains(t, buf.String(), "simulated exec failure")
}
