//go:build unit

package bazelcredhelper

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"

	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/oauth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// Keeps a developer machine's real credentials out of the test.
func isolate(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
}

func useFileStore(t *testing.T) {
	t.Helper()
	keyring.MockInitWithError(keyring.ErrNotFound)
	t.Setenv("HOME", t.TempDir())
}

func TestExpiresLead_StaysBelowRefreshSkew(t *testing.T) {
	assert.Less(t, expiresLead, oauth.RefreshSkew)
}

// testResolver wires a fake refresh into the real resolver, so the tests exercise
// the helper's policy without reaching the network.
func testResolver(ensureFresh func(context.Context) (authpkg.TokenSet, error)) *live.Resolver {
	r := live.Default(nil)
	r.Refresh = func(ctx context.Context, _ authpkg.TokenSet, _ store.Store) (authpkg.TokenSet, error) {
		return ensureFresh(ctx)
	}

	return r
}

func TestResolver_EnvSource_NoRefresh_NoExpiry(t *testing.T) {
	isolate(t)
	envs := map[string]string{
		authpkg.EnvAuthToken:   "env-token",
		authpkg.EnvWorkspaceID: "ws-1",
	}
	ensureFresh := func(context.Context) (authpkg.TokenSet, error) {
		t.Fatal("env credentials have no refresh token; EnsureFresh must not be called")

		return authpkg.TokenSet{}, nil
	}

	got, err := newResolver(testResolver(ensureFresh), envs, nil, "")(t.Context())

	require.NoError(t, err)
	assert.Equal(t, "env-token", got.Token)
	assert.True(t, got.Expiry.IsZero(), "an unknown lifetime must omit the cache hint")
}

// A static PAT has no refresh token, so nothing can renew it.
func TestResolver_LegacyStaticPAT_ServesStaleAndWarns(t *testing.T) {
	isolate(t)
	seedLegacyAuthConfig(t, "bitpat_legacy", "ws-legacy")

	warn := &bytes.Buffer{}
	ensureFresh := func(context.Context) (authpkg.TokenSet, error) {
		t.Fatal("the legacy authConfig source is not store-managed; EnsureFresh must not be called")

		return authpkg.TokenSet{}, nil
	}

	got, err := newResolver(testResolver(ensureFresh), map[string]string{}, warn, "")(t.Context())

	require.NoError(t, err, "a token we cannot refresh is still better than failing the RPC")
	assert.Equal(t, "bitpat_legacy", got.Token)
	assert.Empty(t, warn.String(), "nothing was attempted, so there is nothing to warn about")
}

func TestResolver_StoreManaged_RefreshesAndSetsExpires(t *testing.T) {
	isolate(t)
	seedKeychain(t, "stale-pat", "ws-1")

	patExpiry := time.Now().Add(time.Hour).Truncate(time.Second)
	ensureFresh := func(context.Context) (authpkg.TokenSet, error) {
		return authpkg.TokenSet{AuthToken: "fresh-pat", PATExpiry: patExpiry, WorkspaceID: "ws-1"}, nil
	}

	got, err := newResolver(testResolver(ensureFresh), map[string]string{}, nil, "")(t.Context())

	require.NoError(t, err)
	assert.Equal(t, "fresh-pat", got.Token)
	assert.True(t, got.Expiry.Equal(patExpiry.Add(-expiresLead)), "got %s, want %s", got.Expiry, patExpiry.Add(-expiresLead))
}

// The no-keychain regression: a file-stored credential must take the refresh path.
func TestResolver_FileStore_TakesRefreshPath(t *testing.T) {
	useFileStore(t)
	require.NoError(t, oauthSaveTo(t, store.NewFile(), authpkg.TokenSet{
		AuthToken: "stale-pat", PATExpiry: time.Now().Add(-time.Minute),
		RefreshToken: "r", WorkspaceID: "ws-1",
	}))

	called := false
	patExpiry := time.Now().Add(time.Hour).Truncate(time.Second)
	ensureFresh := func(context.Context) (authpkg.TokenSet, error) {
		called = true

		return authpkg.TokenSet{AuthToken: "fresh-pat", PATExpiry: patExpiry, WorkspaceID: "ws-1"}, nil
	}

	got, err := newResolver(testResolver(ensureFresh), map[string]string{}, nil, "")(t.Context())

	require.NoError(t, err)
	assert.True(t, called, "a file-stored credential must be refreshed, not served verbatim")
	assert.Equal(t, "fresh-pat", got.Token)
}

func TestResolver_RefreshFails_ServesStoredToken_WithShortExpiry(t *testing.T) {
	isolate(t)
	seedKeychain(t, "stored-pat", "ws-1")

	warn := &bytes.Buffer{}
	ensureFresh := func(context.Context) (authpkg.TokenSet, error) {
		return authpkg.TokenSet{}, errors.New("dial tcp: connection refused")
	}

	before := time.Now()
	got, err := newResolver(testResolver(ensureFresh), map[string]string{}, warn, "")(t.Context())

	require.NoError(t, err, "a transient refresh failure must not fail the RPC")
	assert.Equal(t, "stored-pat", got.Token)
	assert.WithinRange(t, got.Expiry, before.Add(staleCacheHint), time.Now().Add(staleCacheHint))
	assert.Empty(t, warn.String(), "a transient error is not actionable; stay quiet")
}

func TestResolver_LoginRequired_WarnsOnce(t *testing.T) {
	isolate(t)
	seedKeychain(t, "stored-pat", "ws-1")

	ensureFresh := func(context.Context) (authpkg.TokenSet, error) {
		return authpkg.TokenSet{}, oauth.ErrLoginRequired
	}

	warn := &bytes.Buffer{}
	resolve := newResolver(testResolver(ensureFresh), map[string]string{}, warn, "")

	got, err := resolve(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "stored-pat", got.Token)

	first := warn.String()
	assert.Contains(t, first, "auth login")
	assert.Equal(t, 1, strings.Count(first, "\n"), "Bazel prints helper stderr per failing RPC; keep it to one line")

	// Every later spawn in the same build must stay quiet.
	for range 5 {
		_, err = resolve(t.Context())
		require.NoError(t, err)
	}
	assert.Equal(t, first, warn.String(), "the warning is rate-limited across spawns")
}

func TestResolver_NoCredentialsAnywhere_ReturnsError(t *testing.T) {
	isolate(t)

	ensureFresh := func(context.Context) (authpkg.TokenSet, error) {
		return authpkg.TokenSet{}, oauth.ErrNotLoggedIn
	}

	_, err := newResolver(testResolver(ensureFresh), map[string]string{}, nil, "")(t.Context())

	require.Error(t, err, "with nothing stored there is no token to fall back to")
}

func seedKeychain(t *testing.T, token, workspaceID string) {
	t.Helper()
	require.NoError(t, store.NewKeychain().Save(authpkg.TokenSet{
		AuthToken: token, WorkspaceID: workspaceID,
		PATExpiry: time.Now().Add(-time.Minute), RefreshToken: "r",
	}))
}

func seedLegacyAuthConfig(t *testing.T, token, workspaceID string) {
	t.Helper()
	cfg := multiplatformconfig.Config{
		AuthConfig: multiplatformconfig.AnalyticsAuthConfig{AuthToken: token, WorkspaceID: workspaceID},
	}
	require.NoError(t, cfg.Save(utils.DefaultOsProxy{}, utils.DefaultEncoderFactory{}))
}

func oauthSaveTo(t *testing.T, s store.Store, c authpkg.TokenSet) error {
	t.Helper()
	_, err := oauth.SaveToWithFallback(s, c, false)

	return err
}
