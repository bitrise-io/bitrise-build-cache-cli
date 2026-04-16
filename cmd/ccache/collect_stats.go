package ccache

import (
	"fmt"

	"github.com/spf13/cobra"

	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/ccache"
)

//nolint:gochecknoglobals
var (
	collectStatsInvocationID  string
	collectStatsParentID      string
	collectStatsDownloadBytes int64
	collectStatsUploadBytes   int64

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

			if err := helper.CollectStats(cmd.Context(), ccachepkg.CollectStatsParams{
				DownloadedBytes: collectStatsDownloadBytes,
				UploadedBytes:   collectStatsUploadBytes,
			}); err != nil {
				return fmt.Errorf("collect stats: %w", err)
			}

			return nil
		},
	}
)

func init() {
	collectStatsCmd.Flags().StringVar(&collectStatsInvocationID, "invocation-id", "", "Invocation ID to report stats under (required)")
	collectStatsCmd.Flags().StringVar(&collectStatsParentID, "parent-id", "", "Parent invocation ID")
	collectStatsCmd.Flags().Int64Var(&collectStatsDownloadBytes, "downloaded-bytes", 0, "Bytes downloaded from cache during this invocation (overridden by session state if helper is running)")
	collectStatsCmd.Flags().Int64Var(&collectStatsUploadBytes, "uploaded-bytes", 0, "Bytes uploaded to cache during this invocation (overridden by session state if helper is running)")

	if err := collectStatsCmd.MarkFlagRequired("invocation-id"); err != nil {
		panic(err)
	}

	storageHelperCmd.AddCommand(collectStatsCmd)
}
