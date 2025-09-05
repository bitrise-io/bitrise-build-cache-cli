package xcode

import (
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd"
)

var xcelerateCommand = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "xcelerate",
	Short: "Bitrise Xcelerate related commands, used for compilation caching.",
	Long:  "Bitrise Xcelerate related commands, used for compilation caching. To activate Xcelerate, use `activate xcelerate` first.",
}

func init() {
	cmd.RootCmd.AddCommand(xcelerateCommand)
}
