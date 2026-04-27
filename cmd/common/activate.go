package common

import (
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/common/childstats"
)

// ActivateCmd represents the activate command
var ActivateCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "activate",
	Short: "Activate various bitrise plugins",
	Long: `Activate Gradle, Bazel, etc. plugins
Call the subcommands with the name of the tool you want to activate plugins for.`,
	PersistentPreRun: func(_ *cobra.Command, _ []string) {
		// Opportunistic sweep of stale child-invocation ledger dirs.
		// Best-effort: failures must not block activation.
		_ = childstats.Sweep(childstats.DefaultSweepTTL)
	},
}

func init() {
	RootCmd.AddCommand(ActivateCmd)
}
