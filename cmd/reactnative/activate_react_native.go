package reactnative

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/xcode"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

var ( //nolint:gochecknoglobals
	activateGradle bool
	activateXcode  bool
	activateCpp    bool
)

//nolint:gochecknoglobals
var activateReactNativeCmd = &cobra.Command{
	Use:   "react-native",
	Short: "Activate Bitrise Build Cache for React Native",
	Long: `Activate Bitrise Build Cache for React Native.
This command activates build cache for all build systems used in React Native projects:

- Gradle (Android builds)
- Xcode (iOS builds)
- C++ via ccache (native modules)

Each can be individually enabled or disabled using flags.
Note: This is a convenience activation method, if your activation requires fine-tuning (ie.: cache-push, cache-validation, etc.) you should use the individual activation calls (ie.: bitrise-build-cache-cli activate gradle).
`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(common.IsDebugLogMode)
		logger.TInfof("Activate Bitrise Build Cache for React Native")

		return ActivateReactNativeCmdFn(
			cmd.Context(),
			logger,
			activateGradle,
			activateXcode,
			activateCpp,
			defaultGradleActivationFn,
			defaultXcodeActivationFn,
			defaultCppActivationFn,
		)
	},
}

func init() {
	common.ActivateCmd.AddCommand(activateReactNativeCmd)
	activateReactNativeCmd.Flags().BoolVar(&activateGradle, "gradle", true, "Activate Gradle build cache (Android).")
	activateReactNativeCmd.Flags().BoolVar(&activateXcode, "xcode", true, "Activate Xcode build cache (iOS).")
	activateReactNativeCmd.Flags().BoolVar(&activateCpp, "cpp", true, "Activate C++ build cache via ccache (native modules).")
}

func defaultGradleActivationFn(logger log.Logger) error {
	gradleHome, err := pathutil.NewPathModifier().AbsPath("~/.gradle")
	if err != nil {
		return fmt.Errorf("expand Gradle home path: %w", err)
	}
	gradleParams := gradleconfig.DefaultActivateGradleParams()
	gradleParams.Cache.Enabled = true
	gradleParams.Cache.PushEnabled = true

	return gradle.ActivateGradleCmdFn(
		logger,
		gradleHome,
		utils.AllEnvs(),
		gradleParams.TemplateInventory,
		func(inventory gradleconfig.TemplateInventory, path string) error {
			return inventory.WriteToGradleInit(
				logger,
				path,
				utils.DefaultOsProxy{},
				gradleconfig.GradleTemplateProxy(),
			)
		},
		gradleconfig.DefaultGradlePropertiesUpdater(),
		gradleParams,
	)
}

func defaultXcodeActivationFn(ctx context.Context, logger log.Logger) error {
	xcodeParams := xcelerate.DefaultParams()
	xcodeParams.DebugLogging = common.IsDebugLogMode
	return xcode.ActivateXcodeCommandFn(
		ctx,
		logger,
		utils.DefaultOsProxy{},
		utils.DefaultCommandFunc(),
		utils.DefaultEncoderFactory{},
		utils.DefaultDecoderFactory{},
		xcodeParams,
		utils.AllEnvs(),
	)
}

func defaultCppActivationFn(ctx context.Context, logger log.Logger) error {
	return ccache.ActivateCppCommandFn(
		ctx,
		logger,
		utils.DefaultOsProxy{},
		utils.DefaultCommandFunc(),
		utils.DefaultEncoderFactory{},
		ccacheconfig.DefaultParams(),
		utils.AllEnvs(),
	)
}

// ActivateReactNativeCmdFn activates build cache for the requested sub-systems.
// Each sub-system activation is provided as an injectable function to allow testing.
func ActivateReactNativeCmdFn(
	ctx context.Context,
	logger log.Logger,
	doGradle, doXcode, doCpp bool,
	gradleFn func(log.Logger) error,
	xcodeFn func(context.Context, log.Logger) error,
	cppFn func(context.Context, log.Logger) error,
) error {
	if doGradle {
		logger.TInfof("Activating Gradle build cache...")
		if err := gradleFn(logger); err != nil {
			return fmt.Errorf("activate Gradle build cache: %w", err)
		}
	}

	if doXcode {
		logger.TInfof("Activating Xcode build cache...")
		if err := xcodeFn(ctx, logger); err != nil {
			return fmt.Errorf("activate Xcode build cache: %w", err)
		}
	}

	if doCpp {
		logger.TInfof("Activating C++ build cache...")
		if err := cppFn(ctx, logger); err != nil {
			return fmt.Errorf("activate C++ build cache: %w", err)
		}
	}

	logger.TInfof("✅ Bitrise Build Cache for React Native activated")

	return nil
}
