package ccache

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/common"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

const (
	activateCpp           = "Activate Bitrise Build Cache for C++"
	ActivateCppSuccessful = "✅ Bitrise Build Cache for C++ activated"
)

//nolint:gochecknoglobals
var activateCppParams = ccacheconfig.DefaultParams()

//nolint:gochecknoglobals
var activateCppCmd = &cobra.Command{
	Use:   "c++",
	Short: "Activate Bitrise Build Cache for C++",
	Long: `Activate Bitrise Build Cache for C++.
This command will:

- Create a config file at ~/.bitrise/cache/ccache/config.json with the ccache storage helper settings.
- Set the CCACHE_BASEDIR, CCACHE_NOHASHDIR, CCACHE_REMOTE_ONLY, CCACHE_REMOTE_STORAGE,
  CMAKE_CXX_COMPILER_LAUNCHER and CMAKE_C_COMPILER_LAUNCHER environment variables via envman.
`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(common.IsDebugLogMode)
		logger.TInfof(activateCpp)

		return ActivateCppCommandFn(
			cmd.Context(),
			logger,
			utils.DefaultOsProxy{},
			utils.DefaultCommandFunc(),
			utils.DefaultEncoderFactory{},
			activateCppParams,
			utils.AllEnvs(),
		)
	},
}

func init() {
	common.ActivateCmd.AddCommand(activateCppCmd)
	activateCppCmd.Flags().StringVar(
		&activateCppParams.BuildCacheEndpoint,
		"cache-endpoint",
		activateCppParams.BuildCacheEndpoint,
		"Build Cache endpoint URL.",
	)
	activateCppCmd.Flags().BoolVar(
		&activateCppParams.PushEnabled,
		"cache-push",
		activateCppParams.PushEnabled,
		"Enable pushing new cache entries.",
	)
	activateCppCmd.Flags().StringVar(
		&activateCppParams.IPCSocketPathOverride,
		"ipc-socket-path",
		activateCppParams.IPCSocketPathOverride,
		"Override the IPC socket path for the ccache storage helper. Defaults to <working-dir>/ccache-ipc.sock.",
	)
	activateCppCmd.Flags().StringVar(
		&activateCppParams.BaseDirOverride,
		"basedir",
		activateCppParams.BaseDirOverride,
		"Override the base directory for ccache (CCACHE_BASEDIR). Defaults to the current working directory.",
	)
}

func ActivateCppCommandFn(
	ctx context.Context,
	logger log.Logger,
	osProxy utils.OsProxy,
	commandFunc utils.CommandFunc,
	encoderFactory utils.EncoderFactory,
	params ccacheconfig.Params,
	envs map[string]string,
) error {
	config, err := ccacheconfig.NewConfig(envs, osProxy, params)
	if err != nil {
		return fmt.Errorf("failed to create ccache config: %w", err)
	}

	config.DebugLogging = common.IsDebugLogMode

	if err := config.Save(logger, osProxy, encoderFactory); err != nil {
		return fmt.Errorf("failed to save ccache config: %w", err)
	}

	baseDir := params.BaseDirOverride
	if baseDir == "" {
		wd, err := osProxy.Getwd()
		if err != nil {
			logger.Warnf("Failed to get working directory for CCACHE_BASEDIR: %s", err)
		} else {
			baseDir = wd
		}
	}

	envVars := map[string]string{
		"CCACHE_BASEDIR":              baseDir,
		"CCACHE_NOHASHDIR":            "true",
		"CCACHE_REMOTE_ONLY":          "true",
		"CCACHE_REMOTE_STORAGE":       config.CRSHRemoteStorageURL(),
		"CMAKE_CXX_COMPILER_LAUNCHER": "ccache",
		"CMAKE_C_COMPILER_LAUNCHER":   "ccache",
	}

	for key, value := range envVars {
		addEnvVarToEnvman(ctx, commandFunc, key, value, logger)
	}

	logger.TInfof(ActivateCppSuccessful)

	return nil
}

func addEnvVarToEnvman(
	ctx context.Context,
	commandFunc utils.CommandFunc,
	key, value string,
	logger log.Logger,
) {
	command := commandFunc(ctx, "envman", "add", "--key", key, "--value", value)
	if output, err := command.CombinedOutput(); err != nil {
		logger.Debugf("Failed to run envman add for %s: %s", key, string(output))

		return
	}

	logger.TInfof("Set %s=%s via envman", key, value)
}
