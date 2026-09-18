//go:build unit

package project_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/project"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

func runScopeCheck(t *testing.T, extra ...string) (int, string) {
	t.Helper()

	stderr := &bytes.Buffer{}
	stdout := &bytes.Buffer{}
	prevOut, prevErr := common.RootCmd.OutOrStdout(), common.RootCmd.ErrOrStderr()
	common.RootCmd.SetOut(stdout)
	common.RootCmd.SetErr(stderr)
	common.RootCmd.SetArgs(append([]string{"project", "scope-check"}, extra...))
	t.Cleanup(func() {
		common.RootCmd.SetOut(prevOut)
		common.RootCmd.SetErr(prevErr)
		common.RootCmd.SetArgs(nil)
		project.ResetForTest()
	})

	err := common.RootCmd.Execute()
	if err == nil {
		return 0, stderr.String()
	}
	var ec common.ExitCodeError
	if errors.As(err, &ec) {
		return ec.Code, stderr.String()
	}
	t.Fatalf("unexpected error type: %v", err)

	return 0, ""
}

func TestScopeCheck_AlwaysMode_ExitsZeroSilently(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := t.TempDir()
	code, stderr := runScopeCheck(t, dir)

	assert.Equal(t, 0, code)
	assert.Empty(t, stderr)
}

func TestScopeCheck_OptInWithoutMarker_ExitsOne(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeMachineOptIn(t, home)

	dir := t.TempDir()
	code, stderr := runScopeCheck(t, dir)

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "project-mode=opt-in")
	assert.Contains(t, stderr, "no .bitrise-build-cache.json")
}

func TestScopeCheck_OptInWithMarker_ExitsZero(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeMachineOptIn(t, home)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, paths.ProjectMarkerFilename), []byte(`{}`), 0o644))

	code, stderr := runScopeCheck(t, dir)

	assert.Equal(t, 0, code)
	assert.Contains(t, stderr, "marker at")
}

func TestScopeCheck_OptInQuietSuppressesStderr(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeMachineOptIn(t, home)

	dir := t.TempDir()

	code, stderr := runScopeCheck(t, "--quiet", dir)

	assert.Equal(t, 1, code)
	assert.Empty(t, stderr)
}

func writeMachineOptIn(t *testing.T, home string) {
	t.Helper()
	p := paths.FromHome(home)
	require.NoError(t, os.MkdirAll(p.BitriseCacheRoot(), 0o755))
	require.NoError(t, os.WriteFile(p.MachineConfigFile(), []byte(`{"project_mode":"opt-in"}`), 0o644))
}
