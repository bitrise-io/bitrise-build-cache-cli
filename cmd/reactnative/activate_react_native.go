package reactnative

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/xcode"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/dependencies"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

//nolint:gochecknoglobals
var (
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
			installDependencies,
			defaultGradleActivationFn,
			defaultXcodeActivationFn,
			defaultCppActivationFn,
			defaultStartStorageHelperFn,
		)
	},
}

func init() {
	common.ActivateCmd.AddCommand(activateReactNativeCmd)
	activateReactNativeCmd.Flags().BoolVar(&activateGradle, "gradle", true, "Activate Gradle build cache (Android).")
	activateReactNativeCmd.Flags().BoolVar(&activateXcode, "xcode", true, "Activate Xcode build cache (iOS).")
	activateReactNativeCmd.Flags().BoolVar(&activateCpp, "cpp", true, "Activate C++ build cache via ccache (native modules).")
}

// BuildGradleActivationFn constructs the Gradle activation function with an injectable underlying call.
func BuildGradleActivationFn(
	activateFn func(log.Logger, string, map[string]string, func(log.Logger, map[string]string, bool, configcommon.BenchmarkPhaseProvider) (gradleconfig.TemplateInventory, error), func(gradleconfig.TemplateInventory, string) error, gradleconfig.GradlePropertiesUpdater, gradleconfig.ActivateGradleParams) error,
) func(log.Logger) error {
	return func(logger log.Logger) error {
		gradleHome, err := pathutil.NewPathModifier().AbsPath("~/.gradle")
		if err != nil {
			return fmt.Errorf("expand Gradle home path: %w", err)
		}
		gradleParams := gradleconfig.DefaultActivateGradleParams()
		gradleParams.Cache.Enabled = true
		gradleParams.Cache.PushEnabled = true

		return activateFn(
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
}

// BuildXcodeActivationFn constructs the Xcode activation function with an injectable underlying call.
func BuildXcodeActivationFn(
	activateFn func(context.Context, log.Logger, utils.OsProxy, utils.CommandFunc, utils.EncoderFactory, utils.DecoderFactory, xcelerate.Params, map[string]string) error,
) func(context.Context, log.Logger) error {
	return func(ctx context.Context, logger log.Logger) error {
		xcodeParams := xcelerate.DefaultParams()
		xcodeParams.DebugLogging = common.IsDebugLogMode

		return activateFn(ctx, logger, utils.DefaultOsProxy{}, utils.DefaultCommandFunc(), utils.DefaultEncoderFactory{}, utils.DefaultDecoderFactory{}, xcodeParams, utils.AllEnvs())
	}
}

// BuildCppActivationFn constructs the C++ activation function with an injectable underlying call.
func BuildCppActivationFn(
	activateFn func(context.Context, log.Logger, utils.OsProxy, utils.CommandFunc, utils.EncoderFactory, ccacheconfig.Params, map[string]string) error,
) func(context.Context, log.Logger) error {
	return func(ctx context.Context, logger log.Logger) error {
		return activateFn(ctx, logger, utils.DefaultOsProxy{}, utils.DefaultCommandFunc(), utils.DefaultEncoderFactory{}, ccacheconfig.DefaultParams(), utils.AllEnvs())
	}
}

// StartStorageHelperDeps holds the injectable dependencies for building a start-storage-helper function.
type StartStorageHelperDeps struct {
	// Executable returns the path to the current binary.
	Executable func() (string, error)
	// StartProcess launches a detached process and returns its PID.
	StartProcess func(name string, args ...string) (int, error)
}

// Build returns a function that starts the ccache storage helper.
func (d StartStorageHelperDeps) Build() func(context.Context, log.Logger) error {
	return func(_ context.Context, logger log.Logger) error {
		binary, err := d.Executable()
		if err != nil {
			return fmt.Errorf("get executable path: %w", err)
		}
		// Use a non-context-aware start so the helper is not killed
		// when the activation command's context is cancelled.
		pid, err := d.StartProcess(binary, "ccache", "storage-helper", "start")
		if err != nil {
			return fmt.Errorf("start ccache storage helper: %w", err)
		}
		logger.TInfof("Ccache storage helper started (pid %d)", pid)

		return nil
	}
}

var (
	defaultGradleActivationFn = BuildGradleActivationFn(gradle.ActivateGradleCmdFn)  //nolint:gochecknoglobals
	defaultXcodeActivationFn  = BuildXcodeActivationFn(xcode.ActivateXcodeCommandFn) //nolint:gochecknoglobals
	defaultCppActivationFn    = BuildCppActivationFn(ccache.ActivateCppCommandFn)    //nolint:gochecknoglobals
)

//nolint:gochecknoglobals
var defaultStartStorageHelperFn = StartStorageHelperDeps{
	Executable: os.Executable,
	StartProcess: func(name string, args ...string) (int, error) {
		cmd := exec.Command(name, args...) //nolint:gosec // intentionally detached: the helper must outlive this command
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return 0, fmt.Errorf("start storage helper process: %w", err)
		}

		return cmd.Process.Pid, nil
	},
}.Build()

// ActivateReactNativeCmdFn activates build cache for the requested sub-systems.
// Each sub-system activation is provided as an injectable function to allow testing.
func ActivateReactNativeCmdFn(
	ctx context.Context,
	logger log.Logger,
	doGradle, doXcode, doCpp bool,
	installDepsFn func(context.Context, log.Logger, bool) error,
	gradleFn func(log.Logger) error,
	xcodeFn func(context.Context, log.Logger) error,
	cppFn func(context.Context, log.Logger) error,
	startStorageHelperFn func(context.Context, log.Logger) error,
) error {
	if err := installDepsFn(ctx, logger, doCpp); err != nil {
		return fmt.Errorf("install dependencies: %w", err)
	}

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

		logger.TInfof("Starting ccache storage helper...")
		if err := startStorageHelperFn(ctx, logger); err != nil {
			return fmt.Errorf("start ccache storage helper: %w", err)
		}
	}

	authConfig, err := configcommon.ReadAuthConfigFromEnvironments(utils.AllEnvs())
	if err != nil {
		return fmt.Errorf("read auth config for multiplatform analytics: %w", err)
	}
	cfg := multiplatformconfig.Config{AuthConfig: authConfig, DebugLogging: common.IsDebugLogMode}
	if err := cfg.Save(utils.DefaultOsProxy{}, utils.DefaultEncoderFactory{}); err != nil {
		return fmt.Errorf("save multiplatform analytics config: %w", err)
	}

	logger.TInfof("✅ Bitrise Build Cache for React Native activated")

	return nil
}

func installDependencies(ctx context.Context, logger log.Logger, doCpp bool) error {
	var tools []dependencies.Tool

	cliTool, err := dependencies.CLITool()
	if err != nil {
		return fmt.Errorf("determine CLI tool: %w", err)
	}
	tools = append(tools, cliTool)

	if doCpp {
		tools = append(tools, dependencies.CcacheTool())
	}

	if err := dependencies.EnsureAll(ctx, tools, logger); err != nil {
		return fmt.Errorf("ensure dependencies: %w", err)
	}

	return nil
}
