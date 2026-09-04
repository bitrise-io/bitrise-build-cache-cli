//go:build unit

package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func TestProjectScopeCheck_noMarkerIsOK(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	d := &Doctor{
		AuthBackends: []store.Store{fakeAuthStore{creds: authpkg.TokenSet{AuthToken: "t", WorkspaceID: "ws-default"}}},
		Envs:         map[string]string{},
	}

	res := d.projectScopeCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "no .bitrise-build-cache.json")
	assert.Contains(t, res.Detail, "machine-wide credential in use")
}

func TestProjectScopeCheck_markerMatchesStoredWorkspaceIsOK(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, `{"workspace":"ws-A"}`)
	t.Chdir(dir)

	d := &Doctor{
		AuthBackends: []store.Store{fakeAuthStore{creds: authpkg.TokenSet{
			AuthToken:   "top-tok",
			WorkspaceID: "ws-default",
			Workspaces: map[string]authpkg.TokenSet{
				"ws-A": {AuthToken: "per-ws-tok", WorkspaceID: "ws-A"},
			},
		}}},
		Envs: map[string]string{},
	}

	res := d.projectScopeCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, `using per-workspace credential for "ws-A"`)
	assert.Contains(t, res.Detail, filepath.Join(dir, paths.ProjectMarkerFilename))
}

func TestProjectScopeCheck_markerWithoutPerWorkspaceTokenIsWarn(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, `{"workspace":"ws-missing"}`)
	t.Chdir(dir)

	d := &Doctor{
		AuthBackends: []store.Store{fakeAuthStore{creds: authpkg.TokenSet{
			AuthToken:   "top-tok",
			WorkspaceID: "ws-default",
		}}},
		Envs: map[string]string{},
	}

	res := d.projectScopeCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, `workspace "ws-missing"`)
	assert.Contains(t, res.Detail, "no per-workspace token is stored")
	assert.Contains(t, res.Detail, "bitrise-build-cache auth set")
	assert.Contains(t, res.Detail, "--workspace-id ws-missing")
}

func TestProjectScopeCheck_malformedMarkerIsError(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, `{not json`)
	t.Chdir(dir)

	d := &Doctor{
		AuthBackends: []store.Store{fakeAuthStore{creds: authpkg.TokenSet{AuthToken: "t", WorkspaceID: "ws"}}},
		Envs:         map[string]string{},
	}

	res := d.projectScopeCheck().Diagnose(context.Background())
	assert.Equal(t, StateError, res.State)
	assert.Contains(t, res.Detail, "malformed")
	assert.Contains(t, res.Detail, "Falling back to machine-wide credential")
}

func TestProjectScopeCheck_markerMissingWorkspaceFieldIsError(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, `{"push":true}`)
	t.Chdir(dir)

	d := &Doctor{
		AuthBackends: []store.Store{fakeAuthStore{creds: authpkg.TokenSet{AuthToken: "t", WorkspaceID: "ws"}}},
		Envs:         map[string]string{},
	}

	res := d.projectScopeCheck().Diagnose(context.Background())
	assert.Equal(t, StateError, res.State)
	assert.Contains(t, res.Detail, "malformed")
}

func TestProjectScopeCheck_toolsSuffixInDetail(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, `{"workspace":"ws-A","tools":{"gradle":{"enabled":true},"xcode":{"enabled":false}}}`)
	t.Chdir(dir)

	d := &Doctor{
		AuthBackends: []store.Store{fakeAuthStore{creds: authpkg.TokenSet{
			AuthToken:   "t",
			WorkspaceID: "ws-default",
			Workspaces: map[string]authpkg.TokenSet{
				"ws-A": {AuthToken: "per-tok", WorkspaceID: "ws-A"},
			},
		}}},
		Envs: map[string]string{},
	}

	res := d.projectScopeCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "enabled=[gradle]")
	assert.Contains(t, res.Detail, "disabled=[xcode]")
}

func TestProjectScopeCheck_walkUpFindsMarkerInParent(t *testing.T) {
	root := t.TempDir()
	writeMarker(t, root, `{"workspace":"ws-parent"}`)
	sub := filepath.Join(root, "nested", "deeper")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	t.Chdir(sub)

	d := &Doctor{
		AuthBackends: []store.Store{fakeAuthStore{creds: authpkg.TokenSet{
			AuthToken:   "t",
			WorkspaceID: "ws-default",
			Workspaces: map[string]authpkg.TokenSet{
				"ws-parent": {AuthToken: "per-tok", WorkspaceID: "ws-parent"},
			},
		}}},
		Envs: map[string]string{},
	}

	res := d.projectScopeCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, `"ws-parent"`)
	assert.Contains(t, res.Detail, filepath.Join(root, paths.ProjectMarkerFilename))
}

func TestRun_includesProjectScopeCheck(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	r := newMinimalDoctor(t)

	report := r.Run(context.Background(), Options{SkipUpdateCheck: true, SkipBackendProbe: true})

	var found bool
	for _, it := range report.Items {
		if it.Name == "project-scope" {
			found = true
		}
	}
	assert.True(t, found, "project-scope check should be part of the default check set")
}

func writeMarker(t *testing.T, dir, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, paths.ProjectMarkerFilename), []byte(body), 0o600))
}
