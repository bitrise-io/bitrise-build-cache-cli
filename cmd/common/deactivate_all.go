package common

import (
	"context"
	"errors"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"
)

//nolint:gochecknoglobals
var deactivateAllDryRun bool

// Fan-out hooks. Exported (via package-scoped vars) so tests can spy on them
// without executing the real per-tool cleanup. Production wiring is inline.
//
//nolint:gochecknoglobals
var (
	deactivateAllReactNativeFn = RemoveReactNativeMarker
	deactivateAllGradleFn      = DeactivateGradle
	deactivateAllBazelFn       = DeactivateBazel
	deactivateAllXcodeFn       = DeactivateXcode
	deactivateAllCcacheFn      = DeactivateCcache
)

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

		return runDeactivateAll(cmd.Context(), logger, deactivateAllDryRun)
	},
}

func runDeactivateAll(ctx context.Context, logger log.Logger, dryRun bool) error {
	var errs []error

	if err := deactivateAllReactNativeFn(logger, dryRun); err != nil {
		errs = append(errs, err)
	}

	if err := deactivateAllGradleFn(logger, dryRun); err != nil {
		errs = append(errs, err)
	}

	if err := deactivateAllBazelFn(logger, dryRun); err != nil {
		errs = append(errs, err)
	}

	if err := deactivateAllXcodeFn(logger, dryRun); err != nil {
		errs = append(errs, err)
	}

	if err := deactivateAllCcacheFn(ctx, logger, dryRun); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func init() {
	DeactivateCmd.AddCommand(DeactivateAllCmd)
	DeactivateAllCmd.Flags().BoolVar(&deactivateAllDryRun, "dry-run", false, "List intended removals without executing them.")
}
