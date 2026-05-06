package reactnative

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	rnpkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/reactnative"
)

type rnActivateResult struct {
	Gradle struct {
		Enabled bool `json:"enabled"`
	} `json:"gradle"`
	Xcode struct {
		Enabled bool `json:"enabled"`
	} `json:"xcode"`
	CPP struct {
		Enabled bool `json:"enabled"`
	} `json:"cpp"`
}

//nolint:gochecknoglobals
var (
	gradleEnabled bool
	xcodeEnabled  bool
	cppEnabled    bool
	rnJSONOutput  bool
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
		logOpts := []log.LoggerOptions{log.WithDebugLog(common.IsDebugLogMode)}
		if rnJSONOutput {
			logOpts = append(logOpts, log.WithOutput(cmd.ErrOrStderr()))
		}

		a := rnpkg.NewActivator(rnpkg.ActivatorParams{
			GradleEnabled: gradleEnabled,
			XcodeEnabled:  xcodeEnabled,
			CppEnabled:    cppEnabled,
			DebugLogging:  common.IsDebugLogMode,
			Logger:        log.NewLogger(logOpts...),
		})

		if err := a.Activate(cmd.Context()); err != nil {
			return fmt.Errorf("activate react-native: %w", err)
		}

		if rnJSONOutput {
			var result rnActivateResult
			result.Gradle.Enabled = gradleEnabled
			result.Xcode.Enabled = xcodeEnabled
			result.CPP.Enabled = cppEnabled

			return common.WriteJSON(cmd.OutOrStdout(), result)
		}

		return nil
	},
}

func init() {
	common.ActivateCmd.AddCommand(activateReactNativeCmd)
	activateReactNativeCmd.Flags().BoolVar(&rnJSONOutput, "json", false, "Emit machine-readable JSON to stdout instead of human-readable output")
	activateReactNativeCmd.Flags().BoolVar(&gradleEnabled, "gradle", true, "Activate Gradle build cache (Android).")
	activateReactNativeCmd.Flags().BoolVar(&xcodeEnabled, "xcode", true, "Activate Xcode build cache (iOS).")
	activateReactNativeCmd.Flags().BoolVar(&cppEnabled, "cpp", true, "Activate C++ build cache via ccache (native modules).")
}
