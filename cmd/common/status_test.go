//go:build unit

package common_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/ccache"
	rnconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/reactnative"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/xcelerate"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
)

// runStatusCmd invokes the registered "status" cobra command with the given
// args and returns stdout, stderr, and the execute error. It redirects $HOME
// for the duration of the call so detection reads from a clean fixture dir.
func runStatusCmd(t *testing.T, home string, args ...string) (string, string, error) {
	t.Helper()

	t.Setenv("HOME", home)

	cmd, _, err := common.RootCmd.Find([]string{"status"})
	require.NoError(t, err)

	// Reset command-local flag state BEFORE the run; cobra holds globals
	// between calls. Doing it up-front (rather than in Cleanup) means a
	// previous test's state can't leak into this one even if that test
	// skipped cleanup.
	require.NoError(t, cmd.Flags().Set("json", "false"))
	require.NoError(t, cmd.Flags().Set("feature", ""))
	require.NoError(t, cmd.Flags().Set("quiet", "false"))

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	prevOut, prevErr := common.RootCmd.OutOrStderr(), common.RootCmd.ErrOrStderr()
	common.RootCmd.SetOut(stdout)
	common.RootCmd.SetErr(stderr)
	common.RootCmd.SetArgs(append([]string{"status"}, args...))

	t.Cleanup(func() {
		common.RootCmd.SetOut(prevOut)
		common.RootCmd.SetErr(prevErr)
	})

	execErr := common.RootCmd.Execute()

	return stdout.String(), stderr.String(), execErr
}

func writeXcodeFixture(t *testing.T, home string, enabled bool) {
	t.Helper()
	dir := filepath.Join(home, ".bitrise-xcelerate")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	payload, err := json.Marshal(xcelerate.Config{BuildCacheEnabled: enabled})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), payload, 0o600))
}

func writeCppFixture(t *testing.T, home string, enabled bool) {
	t.Helper()
	dir := filepath.Join(home, ".bitrise", "cache", "ccache")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	payload, err := json.Marshal(ccacheconfig.Config{Enabled: enabled})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), payload, 0o600))
}

func writeRNFixture(t *testing.T, home string, enabled bool) {
	t.Helper()
	dir := filepath.Join(home, ".bitrise", "cache", "reactnative")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	payload, err := json.Marshal(rnconfig.Config{Enabled: enabled})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), payload, 0o600))
}

func TestStatus_TextTable(t *testing.T) {
	home := t.TempDir()
	writeXcodeFixture(t, home, true)
	writeCppFixture(t, home, true)

	stdout, stderr, err := runStatusCmd(t, home)
	require.NoError(t, err)
	assert.Empty(t, stderr)
	assert.NotContains(t, stdout, "bazel")

	// Per-row assertions: match `<label><whitespace><state>\n` so we catch
	// cross-row contamination (e.g. xcode row claiming "disabled").
	for _, row := range []struct {
		label string
		state string
	}{
		{"gradle", "disabled"},
		{"xcode", "enabled"},
		{"cpp", "enabled"},
		{"react-native", "disabled"},
	} {
		re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(row.label) + `\s+` + row.state + `$`)
		assert.Regexp(t, re, stdout, "row %q should be %s", row.label, row.state)
	}
}

func TestStatus_JSON_Shape(t *testing.T) {
	home := t.TempDir()
	writeRNFixture(t, home, true)

	stdout, _, err := runStatusCmd(t, home, "--json")
	require.NoError(t, err)

	var got map[string]bool
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))

	assert.False(t, got["gradle"])
	assert.False(t, got["xcode"])
	assert.False(t, got["cpp"])
	assert.True(t, got["reactNative"])
	_, hasBazel := got["bazel"]
	assert.False(t, hasBazel)
}

func TestStatus_FeatureBazel_ExitTwo(t *testing.T) {
	home := t.TempDir()

	_, stderr, err := runStatusCmd(t, home, "--feature=bazel", "--quiet")
	require.Error(t, err)
	code, ok := common.HandleStatusExit(err)
	require.True(t, ok)
	assert.Equal(t, 2, code)
	assert.Contains(t, strings.ToLower(stderr), "unknown feature")
}

func TestStatus_Feature_Enabled(t *testing.T) {
	home := t.TempDir()
	writeRNFixture(t, home, true)

	stdout, _, err := runStatusCmd(t, home, "--feature=react-native")
	require.NoError(t, err)
	assert.Equal(t, "enabled\n", stdout)
}

func TestStatus_Feature_Disabled(t *testing.T) {
	home := t.TempDir()

	stdout, _, err := runStatusCmd(t, home, "--feature=react-native")
	require.NoError(t, err)
	assert.Equal(t, "disabled\n", stdout)
}

func TestStatus_FeatureQuiet_Enabled_ExitZero(t *testing.T) {
	home := t.TempDir()
	writeRNFixture(t, home, true)

	stdout, stderr, err := runStatusCmd(t, home, "--feature=react-native", "--quiet")
	require.NoError(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestStatus_FeatureQuiet_Disabled_ExitOne(t *testing.T) {
	home := t.TempDir()

	stdout, stderr, err := runStatusCmd(t, home, "--feature=react-native", "--quiet")
	require.Error(t, err)
	code, ok := common.HandleStatusExit(err)
	require.True(t, ok)
	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestStatus_FeatureJSON_ShapeMatchesFullJSON(t *testing.T) {
	home := t.TempDir()
	writeRNFixture(t, home, true)

	stdout, _, err := runStatusCmd(t, home, "--feature=react-native", "--json")
	require.NoError(t, err)

	var got map[string]bool
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	assert.Equal(t, map[string]bool{"reactNative": true}, got)
}

func TestStatus_Feature_Unknown_ExitTwo(t *testing.T) {
	home := t.TempDir()

	_, stderr, err := runStatusCmd(t, home, "--feature=bogus", "--quiet")
	require.Error(t, err)
	code, ok := common.HandleStatusExit(err)
	require.True(t, ok)
	assert.Equal(t, 2, code)
	// --quiet still prints the rejection for unknown features so the caller
	// can distinguish "disabled" from "invalid".
	assert.Contains(t, strings.ToLower(stderr), "unknown feature")
}

func TestStatus_Quiet_WithoutFeature_IsError(t *testing.T) {
	home := t.TempDir()

	_, stderr, err := runStatusCmd(t, home, "--quiet")
	require.Error(t, err)
	code, ok := common.HandleStatusExit(err)
	require.True(t, ok)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "--quiet")
}

func TestHandleStatusExit_PassesThroughOtherErrors(t *testing.T) {
	_, ok := common.HandleStatusExit(errors.New("unrelated"))
	assert.False(t, ok)
}
