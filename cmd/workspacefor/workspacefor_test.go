//go:build unit

package workspacefor

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	utilsMocks "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils/mocks"
)

// runCmd invokes the registered "workspace-for" cobra command with args
// and returns stdout, stderr, and the execute error (nil unless the
// command chose a non-zero exit path).
func runCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	cmd, _, err := common.RootCmd.Find([]string{"workspace-for"})
	require.NoError(t, err)

	require.NoError(t, cmd.Flags().Set("path", "."))
	require.NoError(t, cmd.Flags().Set("json", "false"))

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	prevOut, prevErr := common.RootCmd.OutOrStderr(), common.RootCmd.ErrOrStderr()
	common.RootCmd.SetOut(stdout)
	common.RootCmd.SetErr(stderr)
	common.RootCmd.SetArgs(append([]string{"workspace-for"}, args...))

	t.Cleanup(func() {
		common.RootCmd.SetOut(prevOut)
		common.RootCmd.SetErr(prevErr)
	})

	execErr := common.RootCmd.Execute()

	return stdout.String(), stderr.String(), execErr
}

func writeMarker(t *testing.T, dir, workspace string) string {
	t.Helper()
	path := filepath.Join(dir, ".bitrise-build-cache.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"workspace":"`+workspace+`"}`), 0o644))

	return path
}

func TestResolveForPath_FoundAtStartDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	markerPath := writeMarker(t, dir, "acme")

	res, err := resolveForPath(dir, utils.DefaultOsProxy{})

	require.NoError(t, err)
	assert.Equal(t, "acme", res.Workspace)
	assert.Equal(t, markerPath, res.MarkerPath)
}

func TestResolveForPath_FoundAtParent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	markerPath := writeMarker(t, root, "acme")
	nested := filepath.Join(root, "a", "b", "c")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	res, err := resolveForPath(nested, utils.DefaultOsProxy{})

	require.NoError(t, err)
	assert.Equal(t, "acme", res.Workspace)
	assert.Equal(t, markerPath, res.MarkerPath)
}

func TestResolveForPath_NoMarker(t *testing.T) {
	t.Parallel()

	proxy := &utilsMocks.OsProxyMock{
		ReadFileIfExistsFunc: func(string) (string, bool, error) {
			return "", false, nil
		},
	}

	_, err := resolveForPath("/tmp/does/not/exist", proxy)

	require.Error(t, err)
	assert.True(t, errors.Is(err, errNoMarker))
}

func TestResolveForPath_ReadError(t *testing.T) {
	t.Parallel()

	proxy := &utilsMocks.OsProxyMock{
		ReadFileIfExistsFunc: func(string) (string, bool, error) {
			return "", true, errors.New("boom")
		},
	}

	_, err := resolveForPath("/tmp/anything", proxy)

	require.Error(t, err)
	assert.False(t, errors.Is(err, errNoMarker))
}

func TestResolveForPath_MissingWorkspaceField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".bitrise-build-cache.json"), []byte(`{"push":true}`), 0o644))

	_, err := resolveForPath(dir, utils.DefaultOsProxy{})

	require.Error(t, err)
	assert.False(t, errors.Is(err, errNoMarker))
}

func TestResolveForPath_RelativePathResolvedAgainstCWD(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "acme")

	prev, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(prev) })

	res, err := resolveForPath(".", utils.DefaultOsProxy{})

	require.NoError(t, err)
	assert.Equal(t, "acme", res.Workspace)
	// Compare basenames only — macOS's /var → /private/var symlink means
	// filepath.Abs(".") after Chdir may not match the tmpdir path literal.
	assert.Equal(t, ".bitrise-build-cache.json", filepath.Base(res.MarkerPath))
}

func TestWorkspaceFor_FoundAtCWD(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "acme")

	prev, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(prev) })

	stdout, stderr, execErr := runCmd(t)
	require.NoError(t, execErr)
	assert.Equal(t, "acme\n", stdout)
	assert.Empty(t, stderr)
}

func TestWorkspaceFor_FoundAtParent_AbsolutePath(t *testing.T) {
	root := t.TempDir()
	writeMarker(t, root, "acme")
	nested := filepath.Join(root, "a", "b")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	stdout, _, execErr := runCmd(t, "--path", nested)
	require.NoError(t, execErr)
	assert.Equal(t, "acme\n", stdout)
}

func TestWorkspaceFor_NoMarker_ExitTwo(t *testing.T) {
	dir := t.TempDir()

	// Also override HOME so the walk-up can't accidentally trip on a
	// developer's real marker somewhere up the tree.
	t.Setenv("HOME", dir)

	stdout, stderr, execErr := runCmd(t, "--path", dir)
	require.Error(t, execErr)
	code, ok := common.HandleStatusExit(execErr)
	require.True(t, ok)
	assert.Equal(t, 2, code)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestWorkspaceFor_MalformedMarker_ExitOne(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".bitrise-build-cache.json"), []byte(`{not json`), 0o644))

	_, stderr, execErr := runCmd(t, "--path", dir)
	require.Error(t, execErr)
	code, ok := common.HandleStatusExit(execErr)
	require.True(t, ok)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "error:")
}

func TestWorkspaceFor_MissingWorkspaceField_ExitOne(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".bitrise-build-cache.json"), []byte(`{"push":true}`), 0o644))

	_, stderr, execErr := runCmd(t, "--path", dir)
	require.Error(t, execErr)
	code, ok := common.HandleStatusExit(execErr)
	require.True(t, ok)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "workspace")
}

func TestWorkspaceFor_JSONOutput(t *testing.T) {
	dir := t.TempDir()
	markerPath := writeMarker(t, dir, "acme")

	stdout, _, execErr := runCmd(t, "--path", dir, "--json")
	require.NoError(t, execErr)

	var got map[string]string
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	assert.Equal(t, "acme", got["workspace"])
	assert.Equal(t, markerPath, got["markerPath"])
}

func TestWorkspaceFor_RelativePathResolved(t *testing.T) {
	root := t.TempDir()
	writeMarker(t, root, "acme")
	nested := filepath.Join(root, "sub")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	prev, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(root))
	t.Cleanup(func() { _ = os.Chdir(prev) })

	stdout, _, execErr := runCmd(t, "--path", "sub")
	require.NoError(t, execErr)
	assert.Equal(t, "acme\n", stdout)
}
