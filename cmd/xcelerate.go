package cmd

import "github.com/spf13/cobra"

var xcelerateCommand = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "xcelerate",
	Short: "Bitrise Xcelerate related commands, used for compilation caching.",
	Long:  "Bitrise Xcelerate related commands, used for compilation caching. To activate Xcelerate, use `activate xcelerate` first.",
}

func init() {
	rootCmd.AddCommand(xcelerateCommand)
}
