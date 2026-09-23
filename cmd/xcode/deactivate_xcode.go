package xcode

import (
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	xcodepkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/xcode"
)

//nolint:gochecknoglobals
var deactivateXcodeDryRun bool

var DeactivateXcodeCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "xcode",
	Short: "Deactivate Bitrise Build Cache for Xcode",
	Long: `Deactivate Bitrise Build Cache for Xcode.
This command will:

- Stop the running xcelerate proxy (best-effort).
- Remove ~/.bitrise-xcelerate/ (config, wrapper scripts, and pinned CLI copy).
- Strip the "Bitrise Xcelerate" export block from ~/.bashrc and ~/.zshrc.

Log files and enrichment queue under ~/.local/state/xcelerate/* are preserved
for debugging. Credentials in ~/.bitrise/analytics/multiplatform/config.json
are owned by "auth logout" and are NOT touched here. If nothing was activated
the command reports "already absent" for each step and returns success.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))
		logger.EnableDebugLog(common.IsDebugLogMode)
		logger.TInfof("Deactivate Bitrise Build Cache for Xcode")

		return xcodepkg.NewDeactivator(xcodepkg.DeactivatorParams{
			DryRun: deactivateXcodeDryRun,
			Logger: logger,
		}).Deactivate(cmd.Context())
	},
}

func init() {
	common.DeactivateCmd.AddCommand(DeactivateXcodeCmd)
	DeactivateXcodeCmd.Flags().BoolVar(&deactivateXcodeDryRun, "dry-run", false, "List intended removals without executing them.")
}
