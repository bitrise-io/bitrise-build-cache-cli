package ccache

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/permhint"
	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/ccache"
)

//nolint:gochecknoglobals
var activateCppParams = ccacheconfig.DefaultParams()

//nolint:gochecknoglobals
var activateCppProjectMode string

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

		// A warmup run has no business deciding machine-scoped policy for whatever
		// build lands on this VM, so it does not write it.
		if !common.Lite {
			if err := common.PersistProjectMode(activateCppProjectMode, logger); err != nil {
				return fmt.Errorf("persist project mode: %w", err)
			}
		}

		// It does still have to READ that policy. An unchanged flag makes this a
		// pure read that returns what the machine already has, and a nil command is
		// how lite asks for exactly that: skipping the call outright would ignore an
		// operator's persisted `cache push = false` and silently fall back to the
		// flag default, which is the opposite of leaving policy alone.
		pushCmd := cmd
		if common.Lite {
			pushCmd = nil
		}

		push, err := common.ResolveAndPersistCachePush(pushCmd, activateCppParams.PushEnabled, logger)
		if err != nil {
			return fmt.Errorf("resolve cache push: %w", err)
		}
		activateCppParams.PushEnabled = push

		activator := ccachepkg.NewActivator(ccachepkg.ActivatorParams{
			BuildCacheEndpoint:    activateCppParams.BuildCacheEndpoint,
			PushEnabled:           activateCppParams.PushEnabled,
			IPCSocketPathOverride: activateCppParams.IPCSocketPathOverride,
			BaseDirOverride:       activateCppParams.BaseDirOverride,
			DebugLogging:          common.DebugFromFlag(),
			Lite:                  common.Lite,
		})

		if err := activator.Activate(cmd.Context()); err != nil {
			permhint.PrintIfApplicable(log.NewLogger(log.WithDebugLog(common.IsDebugLogMode)), err)

			return fmt.Errorf("activate C++ cache: %w", err)
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
	activateCppCmd.Flags().StringVar(&activateCppProjectMode, common.ProjectModeFlagName, "", common.ProjectModeFlagUsage)
}
