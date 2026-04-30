package common

import (
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/common/childstats"
)

// ActivateCmd represents the activate command
var ActivateCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "activate",
	Short: "Activate various bitrise plugins",
	Long: `Activate Gradle, Bazel, etc. plugins
Call the subcommands with the name of the tool you want to activate plugins for.`,
	PersistentPreRun: func(_ *cobra.Command, _ []string) {
		// Cobra runs only the closest ancestor PersistentPreRun, so this overrides
		// RootCmd's CLI-version log line — re-emit it here.
		configcommon.LogCLIVersion(log.NewLogger(log.WithDebugLog(IsDebugLogMode)))

		// Opportunistic sweep of stale child-invocation ledger dirs.
		// Best-effort: failures must not block activation.
		_ = childstats.Sweep(childstats.DefaultSweepTTL)
	},
}

func init() {
	RootCmd.AddCommand(ActivateCmd)
}
