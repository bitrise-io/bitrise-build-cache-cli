//go:build unit

package bazelcredhelper

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	utilsmocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

// A broken CWD (Getwd fails) must warn and fall through to machine-wide auth
// rather than silently paper over the environment.
func TestDiscoverFromCWD_GetwdError_WarnsAndReturnsEmpty(t *testing.T) {
	osProxy := &utilsmocks.OsProxyMock{
		GetwdFunc: func() (string, error) { return "", errors.New("no cwd") },
	}

	warn := &bytes.Buffer{}
	assert.Empty(t, discoverFromCWD(osProxy, warn))
	assert.Contains(t, warn.String(), "cannot resolve CWD")
	assert.Contains(t, warn.String(), "no cwd")
}

// The composed resolver: marker present → per-workspace credential is served.
func TestResolver_MarkerSlug_PicksPerWorkspaceCredential(t *testing.T) {
	fake := &workspaceStoreStub{ts: auth.TokenSet{
		AuthToken:   "machine-tok",
		WorkspaceID: "machine-ws",
		Workspaces: map[string]auth.TokenSet{
			"acme": {AuthToken: "acme-tok", WorkspaceID: "acme"},
		},
	}}

	r := live.Default(nil)
	r.Backends = []store.Store{fake}
	r.AnalyticsBlock = func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false }
	r.Refresh = func(_ context.Context, ts auth.TokenSet, _ store.Store) (auth.TokenSet, error) { return ts, nil }

	got, err := newResolver(r, map[string]string{}, io.Discard, "acme")(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "acme-tok", got.Token)
}

// Marker names a workspace with no matching store entry — live.Resolver falls
// back with a warning; the helper still emits the machine-wide bearer so the
// build is not blocked.
func TestResolver_UnknownWorkspaceSlug_FallsBackToMachineWide(t *testing.T) {
	fake := &workspaceStoreStub{ts: auth.TokenSet{
		AuthToken:   "machine-tok",
		WorkspaceID: "machine-ws",
	}}

	r := live.Default(nil)
	r.Backends = []store.Store{fake}
	r.AnalyticsBlock = func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false }
	r.Refresh = func(_ context.Context, ts auth.TokenSet, _ store.Store) (auth.TokenSet, error) { return ts, nil }

	got, err := newResolver(r, map[string]string{}, io.Discard, "unknown-ws")(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "machine-tok", got.Token)
}

// Empty slug (no marker discovered) leaves today's behaviour intact.
func TestResolver_EmptySlug_UsesMachineWide(t *testing.T) {
	fake := &workspaceStoreStub{ts: auth.TokenSet{
		AuthToken:   "machine-tok",
		WorkspaceID: "machine-ws",
		Workspaces: map[string]auth.TokenSet{
			"acme": {AuthToken: "acme-tok", WorkspaceID: "acme"},
		},
	}}

	r := live.Default(nil)
	r.Backends = []store.Store{fake}
	r.AnalyticsBlock = func() (auth.Credential, auth.Origin, bool) { return auth.Credential{}, auth.Origin{}, false }
	r.Refresh = func(_ context.Context, ts auth.TokenSet, _ store.Store) (auth.TokenSet, error) { return ts, nil }

	got, err := newResolver(r, map[string]string{}, io.Discard, "")(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "machine-tok", got.Token)
}

var _ utils.OsProxy = (*utilsmocks.OsProxyMock)(nil)

type workspaceStoreStub struct {
	ts auth.TokenSet
}

func (s *workspaceStoreStub) Load() (auth.TokenSet, error) { return s.ts, nil }
func (s *workspaceStoreStub) Save(ts auth.TokenSet) error  { s.ts = ts; return nil }
func (s *workspaceStoreStub) Clear() error                 { s.ts = auth.TokenSet{}; return nil }
func (s *workspaceStoreStub) Backend() auth.Backend        { return auth.BackendKeychain }
