//go:build unit

package activateall

import (
	"testing"

	"github.com/stretchr/testify/require"

	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/bazel"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/gradle"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/reactnative"
	_ "github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/xcode"
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
