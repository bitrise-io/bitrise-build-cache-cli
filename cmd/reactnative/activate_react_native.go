package reactnative

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/common"
	rnpkg "github.com/bitrise-io/bitrise-build-cache-cli/pkg/reactnative"
)

//nolint:gochecknoglobals
var (
	gradleEnabled bool
	xcodeEnabled  bool
	cppEnabled    bool
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
		a := rnpkg.NewActivator(rnpkg.ActivatorParams{
			Gradle:       gradleEnabled,
			Xcode:        xcodeEnabled,
			Cpp:          cppEnabled,
			DebugLogging: common.IsDebugLogMode,
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
}
