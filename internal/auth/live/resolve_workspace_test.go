//go:build unit

package live

import (
	"context"
	"fmt"
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

func machineTokenWithWorkspaces() auth.TokenSet {
	return auth.TokenSet{
		AuthToken:   "machine-tok",
		WorkspaceID: "machine-ws",
		Workspaces: map[string]auth.TokenSet{
			"acme": {AuthToken: "acme-tok", WorkspaceID: "acme"},
		},
	}
}

func TestResolveForWorkspace_picksThePerWorkspaceEntry(t *testing.T) {
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: machineTokenWithWorkspaces(), present: true}},
		AnalyticsBlock: noAnalytics(),
	}

	cred, origin, err := r.ResolveForWorkspace(context.Background(), map[string]string{}, "acme")
	require.NoError(t, err)
	assert.Equal(t, "acme-tok", cred.Token)
	assert.Equal(t, "acme", cred.WorkspaceID)
	assert.Equal(t, auth.BackendKeychain, origin.Backend)
}

func TestResolveForWorkspace_emptySlugMatchesResolve(t *testing.T) {
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: machineTokenWithWorkspaces(), present: true}},
		AnalyticsBlock: noAnalytics(),
	}

	cred, _, err := r.ResolveForWorkspace(context.Background(), map[string]string{}, "")
	require.NoError(t, err)
	assert.Equal(t, "machine-tok", cred.Token, "empty slug must not shadow the machine-wide credential")
}

func TestResolveForWorkspace_unknownSlugFallsBackWithWarn(t *testing.T) {
	logger := &captureLogger{}
	r := &Resolver{
		Logger:         logger,
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: machineTokenWithWorkspaces(), present: true}},
		AnalyticsBlock: noAnalytics(),
	}

	cred, _, err := r.ResolveForWorkspace(context.Background(), map[string]string{}, "missing")
	require.NoError(t, err)
	assert.Equal(t, "machine-tok", cred.Token, "unknown workspace must fall back to the machine-wide credential")
	require.NotEmpty(t, logger.warns, "unknown workspace must emit a warning")
	assert.Contains(t, logger.warns[0], "missing")
}

// The store lookup is not gated on where Resolve got its answer: an env-var
// machine-wide token still loses to a per-workspace entry when the caller asks
// for that workspace by slug.
func TestResolveForWorkspace_perWorkspaceEntryWinsOverEnvMachineWide(t *testing.T) {
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: machineTokenWithWorkspaces(), present: true}},
		AnalyticsBlock: noAnalytics(),
	}

	cred, origin, err := r.ResolveForWorkspace(context.Background(), envVars(), "acme")
	require.NoError(t, err)
	assert.Equal(t, "acme-tok", cred.Token, "per-workspace entry must win over the env-var machine-wide token")
	assert.Equal(t, "acme", cred.WorkspaceID)
	assert.Equal(t, auth.BackendKeychain, origin.Backend, "origin follows the store the entry came from, not the env")
}

// Workspaces-only stores are the state `auth set --workspace-id acme` leaves
// behind when nothing was ever written at the machine-wide slot. The old order
// (Resolve first) errored out here before the per-workspace lookup could run.
func TestResolveForWorkspace_workspacesOnlyStoreServesTheEntry(t *testing.T) {
	workspacesOnly := auth.TokenSet{
		Workspaces: map[string]auth.TokenSet{
			"acme": {AuthToken: "acme-tok", WorkspaceID: "acme"},
		},
	}
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: workspacesOnly, present: true}},
		AnalyticsBlock: noAnalytics(),
	}

	cred, origin, err := r.ResolveForWorkspace(context.Background(), map[string]string{}, "acme")
	require.NoError(t, err)
	assert.Equal(t, "acme-tok", cred.Token)
	assert.Equal(t, "acme", cred.WorkspaceID)
	assert.Equal(t, auth.BackendKeychain, origin.Backend)
}

func TestResolveForWorkspace_unknownSlugWithoutMachineWideReturnsError(t *testing.T) {
	logger := &captureLogger{}
	workspacesOnly := auth.TokenSet{
		Workspaces: map[string]auth.TokenSet{
			"acme": {AuthToken: "acme-tok", WorkspaceID: "acme"},
		},
	}
	r := &Resolver{
		Logger:         logger,
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: workspacesOnly, present: true}},
		AnalyticsBlock: noAnalytics(),
	}

	_, _, err := r.ResolveForWorkspace(context.Background(), map[string]string{}, "missing")
	require.ErrorIs(t, err, auth.ErrTokenNotProvided)
	assert.Empty(t, logger.warns, "fall-back warn must not fire when nothing to fall back to")
}

// ResolveNoRefresh returns success with workspacesOnly=true so activate paths can
// skip baking auth into config and let per-build resolvers do the lookup.
func TestResolveNoRefresh_workspacesOnlyStoreReturnsFlag(t *testing.T) {
	workspacesOnly := auth.TokenSet{
		Workspaces: map[string]auth.TokenSet{
			"acme": {AuthToken: "acme-tok", WorkspaceID: "acme"},
		},
	}
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: workspacesOnly, present: true}},
		AnalyticsBlock: noAnalytics(),
	}

	cred, origin, workspacesOnlyFlag, err := r.ResolveNoRefresh(map[string]string{})

	require.NoError(t, err)
	assert.True(t, workspacesOnlyFlag, "workspaces-only store must surface the flag so activate paths skip baking auth")
	assert.Equal(t, auth.Credential{}, cred, "workspaces-only resolve returns a zero credential")
	assert.Equal(t, auth.Origin{}, origin, "workspaces-only resolve returns a zero origin")
}

func TestResolveNoRefresh_populatedTopLevelIsNotWorkspacesOnly(t *testing.T) {
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: machineTokenWithWorkspaces(), present: true}},
		AnalyticsBlock: noAnalytics(),
	}

	_, _, workspacesOnlyFlag, err := r.ResolveNoRefresh(map[string]string{})

	require.NoError(t, err)
	assert.False(t, workspacesOnlyFlag, "populated top-level must not be reported as workspaces-only")
}

func TestResolveNoRefresh_envBeatsWorkspacesOnly(t *testing.T) {
	workspacesOnly := auth.TokenSet{
		Workspaces: map[string]auth.TokenSet{"acme": {AuthToken: "acme-tok", WorkspaceID: "acme"}},
	}
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: workspacesOnly, present: true}},
		AnalyticsBlock: noAnalytics(),
	}

	cred, origin, workspacesOnlyFlag, err := r.ResolveNoRefresh(envVars())

	require.NoError(t, err)
	assert.False(t, workspacesOnlyFlag, "env vars must not surface as workspaces-only")
	assert.Equal(t, envToken, cred.Token)
	assert.Equal(t, auth.BackendEnv, origin.Backend)
}

// The pin is a no-op on scenario B: nothing to persist since the resolver has
// nothing yet, and every downstream reader will pick up the per-workspace entry
// via the runtime marker lookup.
func TestResolvePinned_workspacesOnlyNoOp(t *testing.T) {
	workspacesOnly := auth.TokenSet{
		Workspaces: map[string]auth.TokenSet{"acme": {AuthToken: "acme-tok", WorkspaceID: "acme"}},
	}
	target := &fakeStore{backend: auth.BackendKeychain, ts: workspacesOnly, present: true}
	r := &Resolver{
		Backends:       []store.Store{target},
		AnalyticsBlock: noAnalytics(),
	}

	cred, origin, err := r.ResolvePinned(context.Background(), map[string]string{}, false)

	require.NoError(t, err)
	assert.Equal(t, auth.Credential{}, cred)
	assert.Equal(t, auth.Origin{}, origin)
	assert.Nil(t, target.saved, "workspaces-only pin must not write anything")
}

func TestResolveNoRefreshForWorkspace_emptySlugFallsBackToMachineWide(t *testing.T) {
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: machineTokenWithWorkspaces(), present: true}},
		AnalyticsBlock: noAnalytics(),
	}

	cred, _, matched, err := r.ResolveNoRefreshForWorkspace(context.Background(), map[string]string{}, "")
	require.NoError(t, err)
	assert.False(t, matched, "empty slug means no per-workspace hit")
	assert.Equal(t, "machine-tok", cred.Token, "empty slug must not shadow the machine-wide credential")
}

func TestResolveNoRefreshForWorkspace_perWorkspaceHit(t *testing.T) {
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: machineTokenWithWorkspaces(), present: true}},
		AnalyticsBlock: noAnalytics(),
	}

	cred, origin, matched, err := r.ResolveNoRefreshForWorkspace(context.Background(), map[string]string{}, "acme")
	require.NoError(t, err)
	assert.True(t, matched, "known workspace must report matched=true")
	assert.Equal(t, "acme-tok", cred.Token)
	assert.Equal(t, "acme", cred.WorkspaceID)
	assert.Equal(t, auth.BackendKeychain, origin.Backend)
}

func TestResolveNoRefreshForWorkspace_unknownSlugFallsBackMatchedFalse(t *testing.T) {
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: machineTokenWithWorkspaces(), present: true}},
		AnalyticsBlock: noAnalytics(),
	}

	cred, _, matched, err := r.ResolveNoRefreshForWorkspace(context.Background(), map[string]string{}, "missing")
	require.NoError(t, err)
	assert.False(t, matched, "unknown workspace must report matched=false so the wrapper keeps its own AuthConfig")
	assert.Equal(t, "machine-tok", cred.Token, "unknown workspace must fall back to the machine-wide credential")
}

func TestResolveNoRefreshForWorkspace_noStoreCredentialSurfacesError(t *testing.T) {
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain}},
		AnalyticsBlock: noAnalytics(),
	}

	_, _, matched, err := r.ResolveNoRefreshForWorkspace(context.Background(), map[string]string{}, "acme")
	require.ErrorIs(t, err, auth.ErrTokenNotProvided)
	assert.False(t, matched)
}

// Workspaces-only case for the no-refresh sibling too: with the old (Resolve
// first) order, ResolveNoRefreshForWorkspace would error before ever looking up
// the per-workspace entry.
func TestResolveNoRefreshForWorkspace_workspacesOnlyStoreServesTheEntry(t *testing.T) {
	workspacesOnly := auth.TokenSet{
		Workspaces: map[string]auth.TokenSet{
			"acme": {AuthToken: "acme-tok", WorkspaceID: "acme"},
		},
	}
	r := &Resolver{
		Backends:       []store.Store{&fakeStore{backend: auth.BackendKeychain, ts: workspacesOnly, present: true}},
		AnalyticsBlock: noAnalytics(),
	}

	cred, origin, matched, err := r.ResolveNoRefreshForWorkspace(context.Background(), map[string]string{}, "acme")
	require.NoError(t, err)
	assert.True(t, matched)
	assert.Equal(t, "acme-tok", cred.Token)
	assert.Equal(t, "acme", cred.WorkspaceID)
	assert.Equal(t, auth.BackendKeychain, origin.Backend)
}

type captureLogger struct {
	warns []string
	debug []string
}

func (c *captureLogger) Printf(string, ...any)             {}
func (c *captureLogger) Println()                          {}
func (c *captureLogger) Debugf(format string, args ...any) { c.debug = append(c.debug, fmt.Sprintf(format, args...)) }
func (c *captureLogger) Infof(string, ...any)              {}
func (c *captureLogger) Donef(string, ...any)              {}
func (c *captureLogger) Errorf(string, ...any)             {}
func (c *captureLogger) Warnf(format string, args ...any)  { c.warns = append(c.warns, fmt.Sprintf(format, args...)) }
func (c *captureLogger) TDebugf(string, ...any)            {}
func (c *captureLogger) TInfof(string, ...any)             {}
func (c *captureLogger) TDonef(string, ...any)             {}
func (c *captureLogger) TErrorf(string, ...any)            {}
func (c *captureLogger) TWarnf(format string, args ...any) { c.warns = append(c.warns, fmt.Sprintf(format, args...)) }
func (c *captureLogger) TPrintf(string, ...any)            {}
func (c *captureLogger) EnableDebugLog(bool)               {}

func TestStoredWorkspaceSlugs_singleEntry(t *testing.T) {
	r := &Resolver{
		Backends: []store.Store{&fakeStore{
			backend: auth.BackendKeychain,
			present: true,
			ts: auth.TokenSet{
				Workspaces: map[string]auth.TokenSet{
					"acme": {AuthToken: "acme-tok", WorkspaceID: "acme"},
				},
			},
		}},
	}

	assert.Equal(t, []string{"acme"}, r.StoredWorkspaceSlugs())
}

func TestStoredWorkspaceSlugs_multipleEntriesSortedDeduped(t *testing.T) {
	r := &Resolver{
		Backends: []store.Store{
			&fakeStore{
				backend: auth.BackendKeychain,
				present: true,
				ts: auth.TokenSet{
					Workspaces: map[string]auth.TokenSet{
						"zzz": {AuthToken: "z-tok", WorkspaceID: "zzz"},
						"aaa": {AuthToken: "a-tok", WorkspaceID: "aaa"},
					},
				},
			},
			&fakeStore{
				backend: auth.BackendFile,
				present: true,
				ts: auth.TokenSet{
					Workspaces: map[string]auth.TokenSet{
						"aaa": {AuthToken: "a-tok-dup", WorkspaceID: "aaa"},
						"mmm": {AuthToken: "m-tok", WorkspaceID: "mmm"},
					},
				},
			},
		},
	}

	assert.Equal(t, []string{"aaa", "mmm", "zzz"}, r.StoredWorkspaceSlugs())
}

// A workspace entry without a token isn't usable, so it must not appear in
// the list — nobody should be handed a slug they can't actually resolve.
func TestStoredWorkspaceSlugs_skipsTokenlessEntries(t *testing.T) {
	r := &Resolver{
		Backends: []store.Store{&fakeStore{
			backend: auth.BackendKeychain,
			present: true,
			ts: auth.TokenSet{
				Workspaces: map[string]auth.TokenSet{
					"has-tok": {AuthToken: "t", WorkspaceID: "has-tok"},
					"no-tok":  {WorkspaceID: "no-tok"},
				},
			},
		}},
	}

	assert.Equal(t, []string{"has-tok"}, r.StoredWorkspaceSlugs())
}

func TestStoredWorkspaceSlugs_noEntries(t *testing.T) {
	r := &Resolver{
		Backends: []store.Store{&fakeStore{backend: auth.BackendKeychain}},
	}

	assert.Empty(t, r.StoredWorkspaceSlugs())
}
