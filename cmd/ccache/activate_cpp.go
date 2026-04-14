package ccache

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/ccache"
	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/ccache"
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
		activator := ccachepkg.NewActivator(ccachepkg.ActivatorParams{
			BuildCacheEndpoint:    activateCppParams.BuildCacheEndpoint,
			PushEnabled:           activateCppParams.PushEnabled,
			IPCSocketPathOverride: activateCppParams.IPCSocketPathOverride,
			BaseDirOverride:       activateCppParams.BaseDirOverride,
			DebugLogging:          common.IsDebugLogMode,
		})

		if err := activator.Activate(cmd.Context()); err != nil {
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
		"Override the IPC socket path for the ccache storage helper. Defaults to <working-dir>/ccache-ipc.sock.",
	)
	activateCppCmd.Flags().StringVar(
		&activateCppParams.BaseDirOverride,
		"basedir",
		activateCppParams.BaseDirOverride,
		"Override the base directory for ccache (CCACHE_BASEDIR). Defaults to the current working directory.",
	)
}
