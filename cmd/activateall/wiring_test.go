//go:build unit

package activateall

import (
	"context"
	"io"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/bazel"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/gradle"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/reactnative"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/xcode"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

func TestPlan_EveryStepNamesARealCommandAndItsFlags(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		for _, debug := range []bool{false, true} {
			a := activator{goos: goos, debug: debug}

			for _, s := range a.plan() {
				cmd, rest, err := common.RootCmd.Find(s.args)
				require.NoError(t, err, s.args)
				require.Equal(t, s.args[1], cmd.Name(), s.args)
				require.NoError(t, cmd.ParseFlags(rest), s.args)
			}
		}
	}
}

func isolatedWorkspace(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRADLE_USER_HOME", "")
	t.Setenv(auth.EnvAuthToken, "a-token")
	t.Setenv(auth.EnvWorkspaceID, monitoringOrg)
	t.Setenv(configcommon.EnvSkipEntitlementCheck, "")

	return home
}

func forceEntitlement(t *testing.T, state configcommon.EntitlementState) *int {
	t.Helper()

	asked := 0
	orig := configcommon.EntitlementChecker
	configcommon.EntitlementChecker = func(context.Context, string, auth.Credential, log.Logger) configcommon.EntitlementState {
		asked++

		return state
	}
	t.Cleanup(func() { configcommon.EntitlementChecker = orig })

	return &asked
}

func written(t *testing.T, home string) []string {
	t.Helper()

	var files []string
	require.NoError(t, filepath.WalkDir(home, func(path string, _ fs.DirEntry, err error) error {
		if err == nil && path != home {
			files = append(files, path)
		}

		return err
	}))

	return files
}

// Each activate command must ask before its first write, or a workspace with no
// Build Cache is configured by it anyway.
func TestActivateCommands_WriteNothingForAWorkspaceWithoutEntitlement(t *testing.T) {
	for _, name := range []string{"gradle", "bazel", "xcode", "c++", "react-native"} {
		t.Run(name, func(t *testing.T) {
			home := isolatedWorkspace(t)
			asked := forceEntitlement(t, configcommon.EntitlementNone)

			cmd, _, err := common.ActivateCmd.Find([]string{name})
			require.NoError(t, err)
			require.Equal(t, name, cmd.Name())
			cmd.SetContext(context.Background())
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			done := make(chan error, 1)
			go func() { done <- cmd.RunE(cmd, nil) }()

			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(15 * time.Second):
				t.Fatal("the command kept going instead of stopping at the entitlement gate")
			}

			assert.Equal(t, 1, *asked, "the command never reached the entitlement gate")
			assert.Empty(t, written(t, home))
		})
	}
}

// `activate all` has to leave a skipped workspace untouched: the version check
// and the stats sweep write under the home directory.
func TestActivateAll_SkippedWorkspaceRunsNeitherTheVersionCheckNorTheSweep(t *testing.T) {
	home := isolatedWorkspace(t)
	t.Setenv("CI", "")

	cmd := ActivateAllCmd
	cmd.SetContext(context.Background())

	common.ActivateCmd.PersistentPreRun(cmd, nil)

	assert.Empty(t, written(t, home))
}
