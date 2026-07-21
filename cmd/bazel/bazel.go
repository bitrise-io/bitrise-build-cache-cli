package bazel

import (
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
)

//nolint:gochecknoglobals
var bazelCmd = &cobra.Command{
	Use:   "bazel",
	Short: "Bitrise Build Cache Bazel-related commands.",
	Long:  "Bitrise Build Cache Bazel-related commands. To activate Bazel remote cache, use `activate bazel`.",
}

func init() {
	common.RootCmd.AddCommand(bazelCmd)
}
