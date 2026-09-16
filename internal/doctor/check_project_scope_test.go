//go:build unit

package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func TestProjectScopeCheck_noMarkerIsOK(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	d := &Doctor{Envs: map[string]string{}}

	res := d.projectScopeCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "no .bitrise-build-cache.json")
	assert.Contains(t, res.Detail, "mode=always")
	assert.Contains(t, res.Detail, "would gate this directory: no")
}

func TestProjectScopeCheck_emptyMarkerReportsPath(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, `{}`)
	t.Chdir(dir)

	d := &Doctor{Envs: map[string]string{}}

	res := d.projectScopeCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, filepath.Join(dir, paths.ProjectMarkerFilename))
}

func TestProjectScopeCheck_unknownFieldsIgnored(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, `{"workspace":"acme","push":true}`)
	t.Chdir(dir)

	d := &Doctor{Envs: map[string]string{}}

	res := d.projectScopeCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, filepath.Join(dir, paths.ProjectMarkerFilename))
}

func TestProjectScopeCheck_malformedMarkerIsError(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, `{not json`)
	t.Chdir(dir)

	d := &Doctor{Envs: map[string]string{}}

	res := d.projectScopeCheck().Diagnose(context.Background())
	assert.Equal(t, StateError, res.State)
	assert.Contains(t, res.Detail, "malformed")
}

func TestProjectScopeCheck_walkUpFindsMarkerInParent(t *testing.T) {
	root := t.TempDir()
	writeMarker(t, root, `{}`)
	sub := filepath.Join(root, "nested", "deeper")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	t.Chdir(sub)

	d := &Doctor{Envs: map[string]string{}}

	res := d.projectScopeCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
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

func TestProjectScopeCheck_optInModeWithoutMarkerGates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeMachineMode(t, home, "opt-in")

	build := filepath.Join(home, "unrelated-project")
	require.NoError(t, os.MkdirAll(build, 0o755))
	t.Chdir(build)

	d := &Doctor{Envs: map[string]string{}}

	res := d.projectScopeCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "mode=opt-in")
	assert.Contains(t, res.Detail, "would gate this directory: yes")
}

func TestProjectScopeCheck_corruptMachineConfigSurfacesWarning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := paths.FromHome(home).BitriseCacheRoot()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, paths.BuildCacheMachineConfigFilename), []byte(`{not json`), 0o644))

	build := filepath.Join(home, "some-project")
	require.NoError(t, os.MkdirAll(build, 0o755))
	t.Chdir(build)

	d := &Doctor{Envs: map[string]string{}}

	res := d.projectScopeCheck().Diagnose(context.Background())
	assert.Equal(t, StateWarn, res.State)
	assert.Contains(t, res.Detail, "machine config unreadable")
}

func TestProjectScopeCheck_optInModeWithMarkerDoesNotGate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeMachineMode(t, home, "opt-in")

	build := filepath.Join(home, "opted-in")
	require.NoError(t, os.MkdirAll(build, 0o755))
	writeMarker(t, build, `{}`)
	t.Chdir(build)

	d := &Doctor{Envs: map[string]string{}}

	res := d.projectScopeCheck().Diagnose(context.Background())
	assert.Equal(t, StateOK, res.State)
	assert.Contains(t, res.Detail, "mode=opt-in")
	assert.Contains(t, res.Detail, "would gate this directory: no")
}

func writeMarker(t *testing.T, dir, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, paths.ProjectMarkerFilename), []byte(body), 0o600))
}

func writeMachineMode(t *testing.T, home, mode string) {
	t.Helper()
	dir := paths.FromHome(home).BitriseCacheRoot()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, paths.BuildCacheMachineConfigFilename), []byte(`{"project_mode":"`+mode+`"}`), 0o644))
}
