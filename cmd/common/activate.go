package common

import (
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/common/childstats"
)

// Lite is preboot activation: write the static wiring a build tool needs, and
// nothing that depends on a workspace, a build or a credential. Under Lite the
// CLI keeps no credential and no build identity — whatever the warmup
// environment happens to hold is discarded at the config boundary, so the
// exclusion does not depend on that environment being empty — and it pins
// nothing, queries no benchmark phase, exports through no envman, starts no
// daemon and persists no machine-scoped policy.
//
// Resolution is still attempted, and a credential that is present but malformed
// still fails the activation: that is a broken machine either way. Only its
// absence is tolerated.
//
//nolint:gochecknoglobals
var Lite bool

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
	ActivateCmd.PersistentFlags().BoolVar(&Lite, "lite", false,
		"Write static wiring only, deferring credentials and build metadata to the build tool. For VM warmup, before a build is assigned.")
}
