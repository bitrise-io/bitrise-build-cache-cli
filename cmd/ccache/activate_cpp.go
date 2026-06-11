package ccache

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/ccache"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/permhint"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/refresh"
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
			permhint.PrintIfApplicable(log.NewLogger(log.WithDebugLog(common.IsDebugLogMode)), err)

			return fmt.Errorf("activate C++ cache: %w", err)
		}

		// Register this activation so D1's bump detector can nudge on future
		// CLI upgrades. Best-effort.
		if home, homeErr := os.UserHomeDir(); homeErr == nil {
			configFile := filepath.Join(home, ".bitrise", "cache", "ccache", "config.json")
			if mErr := refresh.Mark(home, refresh.ToolCcache, configFile, configcommon.GetCLIVersion(log.NewLogger())); mErr != nil {
				log.NewLogger().Debugf("refresh registry mark for ccache failed (non-fatal): %s", mErr)
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
