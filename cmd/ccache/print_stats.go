package ccache

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/ccache"
)

//nolint:gochecknoglobals
var (
	printStatsJSONOutput bool

	printStatsCmd = &cobra.Command{
		Use:           "print-stats",
		Short:         "Print current storage helper session statistics",
		Long:          "Prints the current session statistics of the running ccache storage helper without sending analytics or zeroing counters.\n\nExample:\n  bitrise-build-cache ccache storage-helper print-stats\n  bitrise-build-cache ccache storage-helper print-stats --json",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			helper, err := ccachepkg.NewStorageHelper(ccachepkg.StorageHelperParams{})
			if err != nil {
				wrappedErr := fmt.Errorf("create storage helper: %w", err)
				if printStatsJSONOutput {
					_ = common.WriteJSON(cmd.OutOrStdout(), map[string]any{"error": wrappedErr.Error()})
				}

				return wrappedErr
			}

			stats, err := helper.GetStats(cmd.Context())
			if err != nil {
				wrappedErr := fmt.Errorf("get stats: %w", err)
				if printStatsJSONOutput {
					_ = common.WriteJSON(cmd.OutOrStdout(), map[string]any{"error": wrappedErr.Error()})
				}

				return wrappedErr
			}

			if printStatsJSONOutput {
				return common.WriteJSON(cmd.OutOrStdout(), stats)
			}

			printStats(cmd, stats)

			return nil
		},
	}
)

func init() {
	printStatsCmd.Flags().BoolVar(&printStatsJSONOutput, "json", false, "Emit machine-readable JSON to stdout")
	storageHelperCmd.AddCommand(printStatsCmd)
}

func printStats(cmd *cobra.Command, s ccachepkg.Stats) {
	out := cmd.OutOrStdout()

	fmt.Fprintf(out, "Storage helper session stats\n\n")

	if s.InvocationID != "" {
		fmt.Fprintf(out, "Invocation ID: %s\n", s.InvocationID)
	}

	if s.ParentID != "" {
		fmt.Fprintf(out, "Parent ID:     %s\n", s.ParentID)
	}

	fmt.Fprintf(out, "\nTransfer\n")
	fmt.Fprintf(out, "  Downloaded: %s\n", formatBytes(s.DownloadedBytes))
	fmt.Fprintf(out, "  Uploaded:   %s\n", formatBytes(s.UploadedBytes))

	if s.TotalCalls > 0 {
		fmt.Fprintf(out, "\nccache\n")
		fmt.Fprintf(out, "  Total calls: %d\n", s.TotalCalls)
		fmt.Fprintf(out, "  Cache hit:   %d (%.1f%%)\n", s.CacheHit, s.CacheHitRate*100)
		fmt.Fprintf(out, "  Cache miss:  %d\n", s.CacheMiss)
		fmt.Fprintf(out, "  Remote hit:  %d\n", s.RemoteStorageHit)
		fmt.Fprintf(out, "  Remote miss: %d\n", s.RemoteStorageMiss)
	}
}

func formatBytes(b int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)

	switch {
	case b >= gb:
		return fmt.Sprintf("%.2f GiB", float64(b)/gb)
	case b >= mb:
		return fmt.Sprintf("%.2f MiB", float64(b)/mb)
	case b >= kb:
		return fmt.Sprintf("%.2f KiB", float64(b)/kb)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
