package bazel

import (
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
)

//nolint:gochecknoglobals
var bazelCmd = &cobra.Command{
	Use:   "bazel",
	Short: "Bazel-specific utilities (credential helper shim, .bazelrc snippets)",
	Long: `Bazel-specific utilities beyond the activate / deactivate flow.

Use these subcommands to make a committed --credential_helper line portable
across machines that may not have bitrise-build-cache preinstalled.`,
}

func init() {
	common.RootCmd.AddCommand(bazelCmd)
}
