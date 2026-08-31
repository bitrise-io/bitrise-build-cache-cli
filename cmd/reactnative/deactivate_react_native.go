package reactnative

import (
	"errors"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
)

//nolint:gochecknoglobals
var deactivateReactNativeDryRun bool

// DeactivateReactNativeCmd represents the `react-native` subcommand under `deactivate`.
var DeactivateReactNativeCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "react-native",
	Short: "Deactivate Bitrise Build Cache for React Native",
	Long: `Deactivate Bitrise Build Cache for React Native.
This command will:

- Remove the React Native marker at ~/.bitrise/cache/reactnative/config.json.
- Fan out to ` + "`deactivate gradle`" + `, ` + "`deactivate xcode`" + ` and ` + "`deactivate ccache`" + `.

Each step is best-effort — a missing artifact is not an error.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))
		logger.EnableDebugLog(common.IsDebugLogMode)
		logger.TInfof("Deactivate Bitrise Build Cache for React Native")

		var errs []error

		if err := common.RemoveReactNativeMarker(logger, deactivateReactNativeDryRun); err != nil {
			errs = append(errs, err)
		}

		if err := common.DeactivateGradle(logger, deactivateReactNativeDryRun); err != nil {
			errs = append(errs, err)
		}

		if err := common.DeactivateXcode(logger, deactivateReactNativeDryRun); err != nil {
			errs = append(errs, err)
		}

		if err := common.DeactivateCcache(cmd.Context(), logger, deactivateReactNativeDryRun); err != nil {
			errs = append(errs, err)
		}

		return errors.Join(errs...)
	},
}

func init() {
	common.DeactivateCmd.AddCommand(DeactivateReactNativeCmd)
	DeactivateReactNativeCmd.Flags().BoolVar(&deactivateReactNativeDryRun, "dry-run", false, "List intended removals without executing them.")
}
