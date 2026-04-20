package ccache

import (
	"fmt"

	"github.com/spf13/cobra"

	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/ccache"
)

//nolint:gochecknoglobals
var (
	collectStatsInvocationID string
	collectStatsParentID     string

	collectStatsCmd = &cobra.Command{
		Use:          "collect-stats",
		Short:        "Collect and report ccache statistics, then zero the counters",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			helper, err := ccachepkg.NewStorageHelper(ccachepkg.StorageHelperParams{
				InvocationID:       collectStatsInvocationID,
				ParentInvocationID: collectStatsParentID,
			})
			if err != nil {
				return fmt.Errorf("create storage helper: %w", err)
			}

			if err := helper.CollectAndSendStats(cmd.Context(), collectStatsInvocationID, collectStatsParentID); err != nil {
				return fmt.Errorf("collect stats: %w", err)
			}

			return nil
		},
	}
)

func init() {
	collectStatsCmd.Flags().StringVar(&collectStatsInvocationID, "invocation-id", "", "Invocation ID to report stats under (defaults to value from config or internal state)")
	collectStatsCmd.Flags().StringVar(&collectStatsParentID, "parent-id", "", "Parent invocation ID (defaults to value from config or internal state)")

	storageHelperCmd.AddCommand(collectStatsCmd)
}
