//go:build unit

package common_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"

	// Blank imports register each per-tool deactivate subcommand.
	// Without them the `deactivate` root reports zero subcommands.
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
