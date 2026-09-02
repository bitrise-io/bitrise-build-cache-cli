package ccache

import (
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/ccache"
)

//nolint:gochecknoglobals
var (
	deactivateCcacheDryRun    bool
	deactivateCcacheSocketPth string
)

// DeactivateCcacheCmd represents the `ccache` subcommand under `deactivate`.
var DeactivateCcacheCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "ccache",
	Short: "Deactivate Bitrise Build Cache for C++ (ccache)",
	Long: `Deactivate Bitrise Build Cache for C++.
This command will:

- Stop the running ccache storage helper (best-effort).
- Remove ~/.bitrise/cache/ccache/config.json.

Log files under ~/.local/state/ccache/logs are intentionally preserved for
debugging. If nothing was activated the command reports "already absent" for
each step and returns success.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))
		logger.EnableDebugLog(common.IsDebugLogMode)
		logger.TInfof("Deactivate Bitrise Build Cache for C++")

		return ccachepkg.NewDeactivator(ccachepkg.DeactivatorParams{
			DryRun:             deactivateCcacheDryRun,
			SocketPathOverride: deactivateCcacheSocketPth,
			Logger:             logger,
		}).Deactivate(cmd.Context())
	},
}

func init() {
	common.DeactivateCmd.AddCommand(DeactivateCcacheCmd)
	DeactivateCcacheCmd.Flags().BoolVar(&deactivateCcacheDryRun, "dry-run", false, "List intended removals without executing them.")
	DeactivateCcacheCmd.Flags().StringVar(&deactivateCcacheSocketPth, "socket", "", "Path to the ccache IPC socket (defaults to value from config)")
}
