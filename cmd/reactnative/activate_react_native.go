package reactnative

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	rnpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/reactnative"
)

//nolint:gochecknoglobals
var (
	gradleEnabled        bool
	xcodeEnabled         bool
	cppEnabled           bool
	pushEnabled          bool
	disablePrefixMapping bool
	noSwiftCache         bool
	buildCacheSkipFlags  bool
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
Note: This is a convenience activation method, if your activation requires fine-tuning (ie.: cache-validation, etc.) you should use the individual activation calls (ie.: bitrise-build-cache-cli activate gradle).
`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		a := rnpkg.NewActivator(rnpkg.ActivatorParams{
			GradleEnabled:        gradleEnabled,
			XcodeEnabled:         xcodeEnabled,
			CppEnabled:           cppEnabled,
			PushEnabled:          pushEnabled,
			DisablePrefixMapping: disablePrefixMapping,
			NoSwiftCache:         noSwiftCache,
			BuildCacheSkipFlags:  buildCacheSkipFlags,
			DebugLogging:         common.IsDebugLogMode,
		})

		if err := a.Activate(cmd.Context()); err != nil {
			return fmt.Errorf("activate react-native: %w", err)
		}

		return nil
	},
}

func init() {
	common.ActivateCmd.AddCommand(activateReactNativeCmd)
	activateReactNativeCmd.Flags().BoolVar(&gradleEnabled, "gradle", true, "Activate Gradle build cache (Android).")
	activateReactNativeCmd.Flags().BoolVar(&xcodeEnabled, "xcode", true, "Activate Xcode build cache (iOS).")
	activateReactNativeCmd.Flags().BoolVar(&cppEnabled, "cpp", true, "Activate C++ build cache via ccache (native modules).")
	activateReactNativeCmd.Flags().BoolVar(&pushEnabled, "cache-push", true, "Push enabled/disabled. Enabled means the build can also write new entries to the remote cache. Disabled means the build can only read from the remote cache.")
	activateReactNativeCmd.Flags().BoolVar(&disablePrefixMapping, "disable-prefix-mapping", false, "Disable Clang prefix-mapping flags for the Xcode build cache (see `activate xcode --disable-prefix-mapping`).")
	activateReactNativeCmd.Flags().BoolVar(&noSwiftCache, "no-swift-cache", false, "Cache clang/Objective-C compilation only, leaving Swift uncached (see `activate xcode --no-swift-cache`).")
	activateReactNativeCmd.Flags().BoolVar(&buildCacheSkipFlags, "cache-skip-flags", false, "Skip passing cache flags to xcodebuild except the socket path (see `activate xcode --cache-skip-flags`).")
}
