package common

import (
	"context"
	"errors"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	bazelpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/bazel"
	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/ccache"
	gradlepkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/gradle"
	rnpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/reactnative"
	xcodepkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/xcode"
)

//nolint:gochecknoglobals
var deactivateAllDryRun bool

// deactivateFunc — package-scoped so tests can spy on the fan-out.
type deactivateFunc func(context.Context, log.Logger, bool) error

//nolint:gochecknoglobals
var (
	deactivateAllReactNativeFn deactivateFunc = runReactNativeDeactivate
	deactivateAllGradleFn      deactivateFunc = runGradleDeactivate
	deactivateAllBazelFn       deactivateFunc = runBazelDeactivate
	deactivateAllXcodeFn       deactivateFunc = runXcodeDeactivate
	deactivateAllCcacheFn      deactivateFunc = runCcacheDeactivate
)

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
	fns := []deactivateFunc{
		deactivateAllReactNativeFn,
		deactivateAllGradleFn,
		deactivateAllBazelFn,
		deactivateAllXcodeFn,
		deactivateAllCcacheFn,
	}

	var errs []error
	for _, fn := range fns {
		if err := fn(ctx, logger, dryRun); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func runReactNativeDeactivate(ctx context.Context, logger log.Logger, dryRun bool) error {
	return rnpkg.NewDeactivator(rnpkg.DeactivatorParams{DryRun: dryRun, Logger: logger}).Deactivate(ctx) //nolint:wrapcheck
}

func runGradleDeactivate(ctx context.Context, logger log.Logger, dryRun bool) error {
	return gradlepkg.NewDeactivator(gradlepkg.DeactivatorParams{DryRun: dryRun, Logger: logger}).Deactivate(ctx) //nolint:wrapcheck
}

func runBazelDeactivate(ctx context.Context, logger log.Logger, dryRun bool) error {
	return bazelpkg.NewDeactivator(bazelpkg.DeactivatorParams{DryRun: dryRun, Logger: logger}).Deactivate(ctx) //nolint:wrapcheck
}

func runXcodeDeactivate(ctx context.Context, logger log.Logger, dryRun bool) error {
	return xcodepkg.NewDeactivator(xcodepkg.DeactivatorParams{DryRun: dryRun, Logger: logger}).Deactivate(ctx) //nolint:wrapcheck
}

func runCcacheDeactivate(ctx context.Context, logger log.Logger, dryRun bool) error {
	return ccachepkg.NewDeactivator(ccachepkg.DeactivatorParams{DryRun: dryRun, Logger: logger}).Deactivate(ctx) //nolint:wrapcheck
}

func init() {
	DeactivateCmd.AddCommand(DeactivateAllCmd)
	DeactivateAllCmd.Flags().BoolVar(&deactivateAllDryRun, "dry-run", false, "List intended removals without executing them.")
}
