package cmd

import (
	"github.com/spf13/cobra"
)

// ActivateCmd represents the activate command
var ActivateCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "activate",
	Short: "Activate various bitrise plugins",
	Long: `Activate Gradle, Bazel, etc. plugins
Call the subcommands with the name of the tool you want to activate plugins for.`,
}

func init() {
	RootCmd.AddCommand(ActivateCmd)
}
