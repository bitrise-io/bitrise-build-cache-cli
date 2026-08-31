package ccache

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	ccacheipc "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/ccache"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/permhint"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/ccache"
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
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))
		logger.EnableDebugLog(common.IsDebugLogMode)

		activator := ccachepkg.NewActivator(ccachepkg.ActivatorParams{
			BuildCacheEndpoint:    activateCppParams.BuildCacheEndpoint,
			PushEnabled:           activateCppParams.PushEnabled,
			IPCSocketPathOverride: activateCppParams.IPCSocketPathOverride,
			BaseDirOverride:       activateCppParams.BaseDirOverride,
			DebugLogging:          common.IsDebugLogMode,
		})

		if err := activator.Activate(cmd.Context()); err != nil {
			permhint.PrintIfApplicable(log.NewLogger(log.WithDebugLog(common.IsDebugLogMode)), err)

			return fmt.Errorf("activate C++ cache: %w", err)
		}

		// daemon.Ensure skips the service on CI; ccache silently misses every
		// lookup when nothing serves the socket. Best-effort — must not fail activation.
		socketPath := ccacheconfig.ResolveIPCSocketPath(
			activateCppParams.IPCSocketPathOverride, utils.AllEnvs(), utils.DefaultOsProxy{},
		)
		socket := ccacheipc.NewSocket(socketPath)
		if !socket.IsListening() {
			startOpts := []ccacheipc.StartOption{ccacheipc.WithDebug()}
			if invID := utils.AllEnvs()["BITRISE_INVOCATION_ID"]; invID != "" {
				startOpts = append(startOpts, ccacheipc.WithInvocationID(invID))
			}

			if err := socket.Start(startOpts...); err != nil {
				logger.Warnf("Could not start the ccache storage helper: %s", err)
			} else if !socket.AwaitReady() {
				logger.Warnf("The ccache storage helper did not become ready on %s", socketPath)
			} else {
				logger.Debugf("Started the ccache storage helper on %s", socketPath)
			}
		}

		return nil
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
		"Override the IPC socket path for the ccache storage helper. Defaults to $BITRISE_CCACHE_IPC_SOCKET_PATH or <temp-dir>/ccache-ipc.sock.",
	)
	activateCppCmd.Flags().StringVar(
		&activateCppParams.BaseDirOverride,
		"basedir",
		activateCppParams.BaseDirOverride,
		"Override the base directory for ccache (CCACHE_BASEDIR). Defaults to the current working directory.",
	)
}
