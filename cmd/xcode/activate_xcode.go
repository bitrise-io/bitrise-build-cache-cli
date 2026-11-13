package xcode

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/permhint"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const ideFlagName = "ide"

const (
	activateXcode = "Activate Bitrise Build Cache for Xcode"
)

// Re-exported constants for backward compatibility with existing tests.
var ( //nolint:gochecknoglobals // re-exports from internal
	ActivateXcodeSuccessful = xcelerate.ActivateXcodeSuccessful //nolint:gochecknoglobals
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
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))
		logger.EnableDebugLog(common.IsDebugLogMode)
		logger.TInfof(activateXcode)

		activateXcodeParams.DebugLogging = common.DebugEnabled(activateXcodeParams.DebugLogging)
		logger.Infof("Activate Xcode params: %+v", activateXcodeParams)

		if err := common.PersistProjectMode(activateXcodeProjectMode, logger); err != nil {
			return fmt.Errorf("persist project mode: %w", err)
		}
		common.PrintOptInGateHintIfGated(logger, utils.DefaultOsProxy{})

		push, err := common.ResolveAndPersistCachePush(cmd, activateXcodeParams.PushEnabled, logger)
		if err != nil {
			return fmt.Errorf("resolve cache push: %w", err)
		}
		activateXcodeParams.PushEnabled = push

		if !cmd.Flags().Changed(ideFlagName) {
			if existing, err := xcelerate.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{}, utils.AllEnvs()); err == nil {
				activateXcodeParams.IDE = existing.IDE
			}
		}
		warnIrrelevantIDEFlags(cmd, activateXcodeParams, logger)

		if err := xcelerate.Activate(
			cmd.Context(),
			logger,
			utils.DefaultOsProxy{},
			utils.DefaultCommandFunc(),
			utils.DefaultEncoderFactory{},
			utils.DefaultDecoderFactory{},
			activateXcodeParams,
			utils.AllEnvs(),
		); err != nil {
			permhint.PrintIfApplicable(logger, err)

			return err //nolint:wrapcheck // xcelerate.Activate already returns wrapped errors
		}

		configcommon.LogBenchmarkSummary(logger, []string{configcommon.BuildToolXcode})

		return nil
	},
}

//nolint:gochecknoglobals
var activateXcodeParams = xcelerate.DefaultParams()

//nolint:gochecknoglobals
var activateXcodeProjectMode string

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
	activateXcodeCmd.Flags().BoolVar(&activateXcodeParams.NoSwiftCache,
		"no-swift-cache",
		activateXcodeParams.NoSwiftCache,
		`Cache clang/Objective-C compilation only, leaving Swift uncached.
Swift compile caching requires explicit modules, which some projects cannot build under (typically failing with "unable to resolve module dependency").`)
	activateXcodeCmd.Flags().BoolVar(&activateXcodeParams.DisablePrefixMapping,
		"disable-prefix-mapping",
		activateXcodeParams.DisablePrefixMapping,
		`Disable injecting Clang prefix-mapping flags into xcodebuild. Prefix mapping canonicalizes rotating source/DerivedData paths so compilation cache keys stay stable; disable only if it causes issues.`)
	activateXcodeCmd.Flags().BoolVar(&activateXcodeParams.RemoteCacheCompression,
		"remote-cache-compression",
		activateXcodeParams.RemoteCacheCompression,
		"Compress cache blobs with zstd. On by default; --remote-cache-compression=false turns it off.")
	activateXcodeCmd.Flags().Int64Var(&activateXcodeParams.RemoteCacheCompressionThreshold,
		"remote-cache-compression-threshold",
		activateXcodeParams.RemoteCacheCompressionThreshold,
		"Smallest blob, in bytes, that --remote-cache-compression compresses; smaller blobs are sent uncompressed.")

	activateXcodeCmd.Flags().StringVar(&activateXcodeProjectMode, common.ProjectModeFlagName, "", common.ProjectModeFlagUsage)

	activateXcodeCmd.Flags().BoolVar(&activateXcodeParams.IDE,
		ideFlagName,
		activateXcodeParams.IDE,
		`Activate for Xcode.app IDE builds. Enables the wrapperless pipeline and skips installing the xcodebuild/xcrun PATH wrappers.
Omit (default) for xcodebuild CLI / CI flow. Value is sticky across activations; pass --ide=false to flip back.`)
}

func warnIrrelevantIDEFlags(cmd *cobra.Command, params xcelerate.Params, logger log.Logger) {
	if !params.IDE {
		return
	}

	for _, name := range []string{"xcode-path", "xcrun-path", "timestamps"} {
		if cmd.Flags().Changed(name) {
			logger.Warnf("--%s has no effect with --ide; IDE mode skips the xcodebuild/xcrun wrapper.", name)
		}
	}
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
