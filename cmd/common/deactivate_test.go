//go:build unit

package common_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"

	// Blank imports register each per-tool deactivate subcommand via init().
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/bazel"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/ccache"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/gradle"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/reactnative"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/xcode"
)

func TestDeactivate_HelpListsAllSubcommands(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	prevOut, prevErr := common.RootCmd.OutOrStderr(), common.RootCmd.ErrOrStderr()
	common.RootCmd.SetOut(stdout)
	common.RootCmd.SetErr(stderr)
	common.RootCmd.SetArgs([]string{"deactivate", "--help"})

	t.Cleanup(func() {
		common.RootCmd.SetOut(prevOut)
		common.RootCmd.SetErr(prevErr)
	})

	require.NoError(t, common.RootCmd.Execute())

	out := stdout.String()
	for _, sub := range []string{"all", "gradle", "bazel", "ccache", "xcode", "react-native"} {
		assert.Contains(t, out, sub, "deactivate --help must list subcommand %q", sub)
	}
}

// Guards against a future change slipping an unconditional os.Remove past the
// dry-run gate.
func TestDeactivateAll_DryRun_NoFilesystemMutation(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("GRADLE_USER_HOME", "")

	seed := map[string]string{
		filepath.Join(tmpHome, ".bashrc"):                      "# [start] Bitrise Xcelerate\nexport PATH=/x:$PATH\n# [end] Bitrise Xcelerate\n",
		filepath.Join(tmpHome, ".zshrc"):                       "user=alice\n",
		filepath.Join(tmpHome, ".bazelrc"):                     "# [start] generated-by-bitrise-build-cache\nbuild --remote_cache=x\n# [end] generated-by-bitrise-build-cache\n",
		filepath.Join(tmpHome, ".gradle", "gradle.properties"): "# [start] generated-by-bitrise-build-cache\norg.gradle.caching=true\n# [end] generated-by-bitrise-build-cache\n",
	}
	for path, content := range seed {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	prevOut, prevErr := common.RootCmd.OutOrStderr(), common.RootCmd.ErrOrStderr()
	common.RootCmd.SetOut(stdout)
	common.RootCmd.SetErr(stderr)
	common.RootCmd.SetArgs([]string{"deactivate", "all", "--dry-run"})

	t.Cleanup(func() {
		common.RootCmd.SetOut(prevOut)
		common.RootCmd.SetErr(prevErr)
	})

	require.NoError(t, common.RootCmd.Execute(), "deactivate all --dry-run must exit 0")

	for path, want := range seed {
		got, err := os.ReadFile(path) //nolint:gosec // seeded tmp path
		require.NoError(t, err, "seeded file must still exist after dry-run: %s", path)
		assert.Equalf(t, want, string(got), "dry-run must not mutate %s", path)
	}
}

// Guards against a future change silently dropping a tool from the fan-out list.
func TestDeactivateAll_FanOutInvokesEveryTool(t *testing.T) {
	counts := struct {
		rn, gradle, bazel, xcode, ccache int
	}{}

	restore := common.SwapDeactivateAllFansForTest(
		func(context.Context, log.Logger, bool) error { counts.rn++; return nil },
		func(context.Context, log.Logger, bool) error { counts.gradle++; return nil },
		func(context.Context, log.Logger, bool) error { counts.bazel++; return nil },
		func(context.Context, log.Logger, bool) error { counts.xcode++; return nil },
		func(context.Context, log.Logger, bool) error { counts.ccache++; return nil },
	)
	t.Cleanup(restore)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	prevOut, prevErr := common.RootCmd.OutOrStderr(), common.RootCmd.ErrOrStderr()
	common.RootCmd.SetOut(stdout)
	common.RootCmd.SetErr(stderr)
	common.RootCmd.SetArgs([]string{"deactivate", "all"})

	t.Cleanup(func() {
		common.RootCmd.SetOut(prevOut)
		common.RootCmd.SetErr(prevErr)
	})

	require.NoError(t, common.RootCmd.Execute())

	assert.Equal(t, 1, counts.rn, "react-native fan-out must fire exactly once")
	assert.Equal(t, 1, counts.gradle, "gradle fan-out must fire exactly once")
	assert.Equal(t, 1, counts.bazel, "bazel fan-out must fire exactly once")
	assert.Equal(t, 1, counts.xcode, "xcode fan-out must fire exactly once")
	assert.Equal(t, 1, counts.ccache, "ccache fan-out must fire exactly once")
}
