package reactnative

import (
	"context"
	"errors"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/ccache"
	gradlepkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/gradle"
	rnpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/reactnative"
	xcodepkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/xcode"
)

//nolint:gochecknoglobals
var deactivateReactNativeDryRun bool

// deactivator is the minimal interface every per-tool Deactivator satisfies.
// Declared locally so cmd/reactnative can hold both the RN marker Deactivator
// and the Gradle/Xcode/ccache ones in one slice.
type deactivator interface {
	Deactivate(ctx context.Context) error
}

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

		dryRun := deactivateReactNativeDryRun
		steps := []deactivator{
			rnpkg.NewDeactivator(rnpkg.DeactivatorParams{DryRun: dryRun, Logger: logger}),
			gradlepkg.NewDeactivator(gradlepkg.DeactivatorParams{DryRun: dryRun, Logger: logger}),
			xcodepkg.NewDeactivator(xcodepkg.DeactivatorParams{DryRun: dryRun, Logger: logger}),
			ccachepkg.NewDeactivator(ccachepkg.DeactivatorParams{DryRun: dryRun, Logger: logger}),
		}

		var errs []error
		for _, d := range steps {
			if err := d.Deactivate(cmd.Context()); err != nil {
				errs = append(errs, err)
			}
		}

		return errors.Join(errs...)
	},
}

func init() {
	common.DeactivateCmd.AddCommand(DeactivateReactNativeCmd)
	DeactivateReactNativeCmd.Flags().BoolVar(&deactivateReactNativeDryRun, "dry-run", false, "List intended removals without executing them.")
}
