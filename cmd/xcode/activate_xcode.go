package xcode

import (
	"context"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

const (
	activateXcode = "Activate Bitrise Build Cache for Xcode"
)

// Re-exported constants for backward compatibility with existing tests.
var ( //nolint:gochecknoglobals // re-exports from internal
	ActivateXcodeSuccessful = xcelerate.ActivateXcodeSuccessful //nolint:gochecknoglobals
	AddXcelerateToPath      = xcelerate.AddXcelerateToPath      //nolint:gochecknoglobals
	ErrFmtCreateXcodeConfig = xcelerate.ErrFmtCreateXcodeConfig //nolint:gochecknoglobals
)

//go:generate moq -stub -out mocks/config_mock.go -pkg mocks . XcelerateConfig
type XcelerateConfig interface {
	Save(logger log.Logger, os utils.OsProxy, encoderFactory utils.EncoderFactory) error
}

// activateXcodeCmd represents the `xcode` subcommand under `activate`
var activateXcodeCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "xcode",
	Short: "Activate Bitrise Build Cache for Xcode",
	Long: `Activate Bitrise Build Cache for Xcode.
This command will:

- Create a config file at ~/.bitrise-xcelerate/config.json with the Xcode proxy and wrapper versions.
- Download an executable proxy to enable xcode compilation cache connecting to the Bitrise Build Cache.
- Create an executable wrapper for xcodebuild that will use the proxy to connect to the Bitrise Build Cache.
`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(common.IsDebugLogMode)
		logger.TInfof(activateXcode)

		activateXcodeParams.DebugLogging = common.IsDebugLogMode
		logger.Infof("Activate Xcode params: %+v", activateXcodeParams)

		return xcelerate.Activate(
			cmd.Context(),
			logger,
			utils.DefaultOsProxy{},
			utils.DefaultCommandFunc(),
			utils.DefaultEncoderFactory{},
			utils.DefaultDecoderFactory{},
			activateXcodeParams,
			utils.AllEnvs(),
		)
	},
}

//nolint:gochecknoglobals
var activateXcodeParams = xcelerate.DefaultParams()

func init() {
	common.ActivateCmd.AddCommand(activateXcodeCmd)
	activateXcodeCmd.Flags().StringVar(
		&activateXcodeParams.ProxySocketPathOverride,
		"proxy-socket-path",
		activateXcodeParams.ProxySocketPathOverride,
		"Override the proxy socket path. This is useful for testing purposes.",
	)
	activateXcodeCmd.Flags().BoolVar(&activateXcodeParams.BuildCacheEnabled,
		"cache",
		activateXcodeParams.BuildCacheEnabled,
		"Activate xcode compilation cache.")
	activateXcodeCmd.Flags().StringVar(&activateXcodeParams.BuildCacheEndpoint,
		"cache-endpoint",
		activateXcodeParams.BuildCacheEndpoint,
		"Build Cache endpoint URL.")
	activateXcodeCmd.Flags().BoolVar(&activateXcodeParams.PushEnabled,
		"cache-push",
		activateXcodeParams.PushEnabled,
		"Enable pushing new cache entries")
	activateXcodeCmd.Flags().StringVar(&activateXcodeParams.XcodePathOverride,
		"xcode-path",
		activateXcodeParams.XcodePathOverride,
		`Override the xcodebuild path. By default it will use the $(which xcodebuild) command to determine the path, and if that fails, it will use the default path: /usr/bin/xcodebuild.

Useful if there are multiple Xcode versions installed and you want to use a specific one.`,
	)
	activateXcodeCmd.Flags().StringVar(&activateXcodeParams.XcrunPathOverride,
		"xcrun-path",
		activateXcodeParams.XcrunPathOverride,
		`Override the xcrun path. By default it will use the $(which xcrun) command to determine the path, and if that fails, it will use the default path: /usr/bin/xcrun.

Useful if there are multiple Xcode versions installed and you want to use a specific one.`,
	)

	activateXcodeCmd.Flags().BoolVar(&activateXcodeParams.Silent,
		"silent",
		activateXcodeParams.Silent,
		"Removes all stdout/err logging from the wrapper and proxy. Only xcodebuild logs will be logged.")
	activateXcodeCmd.Flags().BoolVar(&activateXcodeParams.XcodebuildTimestampsEnabled,
		"timestamps",
		activateXcodeParams.XcodebuildTimestampsEnabled,
		"Enable xcodebuild timestamps. This will add timestamps to the xcodebuild output.")
	activateXcodeCmd.Flags().BoolVar(&activateXcodeParams.BuildCacheSkipFlags,
		"cache-skip-flags",
		activateXcodeParams.BuildCacheSkipFlags,
		`Skip passing cache flags to xcodebuild except the COMPILATION_CACHE_REMOTE_SERVICE_PATH.
Cache will have to be enabled manually in the Xcode project settings.`)
}

// ActivateXcodeCommandFn is a backward-compatible wrapper around xcelerate.Activate.
// Prefer xcelerate.Activate directly.
func ActivateXcodeCommandFn(
	ctx context.Context,
	logger log.Logger,
	osProxy utils.OsProxy,
	commandFunc utils.CommandFunc,
	encoderFactory utils.EncoderFactory,
	decoderFactory utils.DecoderFactory,
	activateParams xcelerate.Params,
	envs map[string]string,
) error {
	return xcelerate.Activate(ctx, logger, osProxy, commandFunc, encoderFactory, decoderFactory, activateParams, envs) //nolint:wrapcheck // thin wrapper, error context added by caller
}
