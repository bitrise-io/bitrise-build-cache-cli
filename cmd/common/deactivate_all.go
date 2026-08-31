package common

import (
	"errors"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"
)

//nolint:gochecknoglobals
var deactivateAllDryRun bool

// DeactivateAllCmd fans out `deactivate` to every supported tool.
var DeactivateAllCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:          "all",
	Short:        "Deactivate Bitrise Build Cache for all supported tools",
	Long:         `Deactivate Bitrise Build Cache for Gradle, Bazel, ccache, Xcode and React Native. Each step is best-effort — a missing artifact is not an error.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(IsDebugLogMode))
		logger.EnableDebugLog(IsDebugLogMode)
		logger.TInfof("Deactivate Bitrise Build Cache for all tools")

		var errs []error

		if err := DeactivateReactNativeMarker(logger, deactivateAllDryRun); err != nil {
			errs = append(errs, err)
		}

		if err := DeactivateGradle(logger, deactivateAllDryRun); err != nil {
			errs = append(errs, err)
		}

		if err := DeactivateBazel(logger, deactivateAllDryRun); err != nil {
			errs = append(errs, err)
		}

		if err := DeactivateXcode(logger, deactivateAllDryRun); err != nil {
			errs = append(errs, err)
		}

		if err := DeactivateCcache(cmd.Context(), logger, deactivateAllDryRun); err != nil {
			errs = append(errs, err)
		}

		return errors.Join(errs...)
	},
}

func init() {
	DeactivateCmd.AddCommand(DeactivateAllCmd)
	DeactivateAllCmd.Flags().BoolVar(&deactivateAllDryRun, "dry-run", false, "List intended removals without executing them.")
}
