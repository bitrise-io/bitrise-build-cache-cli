package common

import (
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/common/childstats"
)

var ActivateCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "activate",
	Short: "Activate various bitrise plugins",
	Long: `Activate Gradle, Bazel, etc. plugins
Call the subcommands with the name of the tool you want to activate plugins for.`,
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		// Cobra only runs the closest ancestor PersistentPreRun — re-emit the CLI-version log here.
		configcommon.LogCLIVersion(log.NewLogger(log.WithDebugLog(IsDebugLogMode)))

		_ = childstats.Sweep(childstats.DefaultSweepTTL)

		if !ShouldSkipVersionCheck(cmd) {
			RunVersionCheck(cmd)
		}
	},
}

func init() {
	RootCmd.AddCommand(ActivateCmd)
}
