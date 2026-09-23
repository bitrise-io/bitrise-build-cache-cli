//go:build unit

package live

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

func noAnalytics() func() (auth.Credential, auth.Origin, bool) {
	return func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false }
}

// A workspace-less login is the state `auth login --no-workspace` leaves behind.
func workspacelessLogin() *fakeStore {
	return &fakeStore{
		backend: auth.BackendKeychain,
		present: true,
		ts:      auth.TokenSet{AuthToken: "pat", RefreshToken: "refresh"},
	}
}

func TestResolveReportsWorkspaceNotSelected(t *testing.T) {
	r := &Resolver{
		Backends:       []store.Store{workspacelessLogin()},
		AnalyticsBlock: noAnalytics(),
	}

	_, _, err := r.Resolve(context.Background(), map[string]string{})
	require.ErrorIs(t, err, auth.ErrWorkspaceNotSelected)
}

func TestResolveTokenOnlyServesTheWorkspacelessLogin(t *testing.T) {
	r := &Resolver{
		Backends:       []store.Store{workspacelessLogin()},
		AnalyticsBlock: noAnalytics(),
		Refresh: func(_ context.Context, ts auth.TokenSet, _ store.Store) (auth.TokenSet, error) {
			return ts, nil
		},
	}

	cred, origin, err := r.ResolveTokenOnly(context.Background(), map[string]string{})
	require.NoError(t, err)
	assert.Equal(t, "pat", cred.Token)
	assert.Empty(t, cred.WorkspaceID)
	assert.Equal(t, auth.BackendKeychain, origin.Backend)
}

// Env vars still win, so a workspace-less login can't hijack a scripted run.
func TestResolveTokenOnlyKeepsEnvPrecedence(t *testing.T) {
	r := &Resolver{
		Backends:       []store.Store{workspacelessLogin()},
		AnalyticsBlock: noAnalytics(),
	}

	cred, origin, err := r.ResolveTokenOnly(context.Background(), envVars())
	require.NoError(t, err)
	assert.Equal(t, envToken, cred.Token)
	assert.Equal(t, envWS, cred.WorkspaceID)
	assert.Equal(t, auth.BackendEnv, origin.Backend)
}

// Nothing stored at all is still the "set the env vars" error, not the picker one.
func TestResolveWithNothingStoredReportsMissingEnv(t *testing.T) {
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain}},
		AnalyticsBlock: noAnalytics(),
	}

	_, _, err := r.Resolve(context.Background(), map[string]string{})
	require.ErrorIs(t, err, auth.ErrTokenNotProvided)
}
