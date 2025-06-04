package cmd

import (
    "github.com/spf13/cobra"
)

// activateCmd represents the activate command
var activateCmd = &cobra.Command{ //nolint:gochecknoglobals
    Use:   "activate",
    Short: "Activate various bitrise plugins",
    Long: `Activate Gradle, Bazel, etc. Plugins
Call the subcommands with the name of the tool you want to activate plugins for.`,
}

func init() {
    rootCmd.AddCommand(activateCmd)
}
