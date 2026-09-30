package reactnative

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
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
	projectMode          string
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
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))
		if err := common.RejectLocalOnlyFlags(projectMode); err != nil {
			return fmt.Errorf("invalid flags: %w", err)
		}

		// Before anything is written: a workspace with no Build Cache cannot use it,
		// and activating would spend an analytics invocation saying so.
		if common.SkipForEntitlement(cmd.Context(), logger) {
			return nil
		}

		// A warmup run has no business deciding machine-scoped policy for whatever
		// build lands on this VM, so it does not write it.
		if !common.Lite {
			if err := common.PersistProjectMode(projectMode, logger); err != nil {
				return fmt.Errorf("persist project mode: %w", err)
			}
		}

		// It does still have to READ that policy. An unchanged flag makes this a
		// pure read: the persisted value, or the built-in default when the machine
		// has none, which lite then bakes in. A nil command is how lite asks for that:
		// skipping the call outright would ignore an operator's persisted
		// `cache push = false`, which is the opposite of leaving policy alone.
		pushCmd := cmd
		if common.Lite {
			pushCmd = nil
		}

		push, err := common.ResolveAndPersistCachePush(pushCmd, pushEnabled, logger)
		if err != nil {
			return fmt.Errorf("resolve cache push: %w", err)
		}
		pushEnabled = push

		a := rnpkg.NewActivator(rnpkg.ActivatorParams{
			GradleEnabled:        gradleEnabled,
			XcodeEnabled:         xcodeEnabled,
			CppEnabled:           cppEnabled,
			PushEnabled:          pushEnabled,
			DisablePrefixMapping: disablePrefixMapping,
			NoSwiftCache:         noSwiftCache,
			BuildCacheSkipFlags:  buildCacheSkipFlags,
			DebugLogging:         common.DebugFromFlag(),
			Lite:                 common.Lite,
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
	activateReactNativeCmd.Flags().StringVar(&projectMode, common.ProjectModeFlagName, "", common.ProjectModeFlagUsage)
}
