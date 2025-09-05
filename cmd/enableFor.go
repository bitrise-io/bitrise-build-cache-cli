package cmd

import (
	"github.com/spf13/cobra"
)

// enableForCmd represents the enableFor command
var EnableForCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "enable-for",
	Short: "Enable build cache for Gradle, Bazel, etc.",
	Long: `Enable build cache for Gradle, Bazel, etc.
Call the subcommands with the name of the tool you want to enable build cache for.`,
}

func init() {
	RootCmd.AddCommand(EnableForCmd)
}
