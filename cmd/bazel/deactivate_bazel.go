package bazel

import (
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	bazelpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/bazel"
)

//nolint:gochecknoglobals
var deactivateBazelDryRun bool

var DeactivateBazelCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "bazel",
	Short: "Deactivate Bitrise Build Cache for Bazel",
	Long: `Deactivate Bitrise Build Cache for Bazel.
This command will:

- Strip the "# [start/end] generated-by-bitrise-build-cache" block from ~/.bazelrc.
- Remove ~/.bitrise/cache/bazel/config.json.

The .bazelrc file itself is preserved. If nothing was activated the command
reports "already absent" for each step and returns success.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))
		logger.EnableDebugLog(common.IsDebugLogMode)
		logger.TInfof("Deactivate Bitrise Build Cache for Bazel")

		return bazelpkg.NewDeactivator(bazelpkg.DeactivatorParams{
			DryRun: deactivateBazelDryRun,
			Logger: logger,
		}).Deactivate(cmd.Context())
	},
}

func init() {
	common.DeactivateCmd.AddCommand(DeactivateBazelCmd)
	DeactivateBazelCmd.Flags().BoolVar(&deactivateBazelDryRun, "dry-run", false, "List intended removals without executing them.")
}
