package common

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/common/childstats"
)

//nolint:gochecknoglobals
var (
	reportChildParentID       string
	reportChildChildID        string
	reportChildBuildTool      string
	reportChildHitRate        float32
	reportChildHits           int64
	reportChildTotal          int64
	reportChildBenchmarkPhase string
)

//nolint:gochecknoglobals
var reportChildStatsCmd = &cobra.Command{
	Use:          "report-child-stats",
	Short:        "Write a child invocation's cache stats to the local ledger for parent-side aggregation",
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		if reportChildParentID == "" || reportChildChildID == "" {
			return nil
		}

		hitRate := reportChildHitRate
		if hitRate == 0 && reportChildTotal > 0 {
			hitRate = float32(reportChildHits) / float32(reportChildTotal)
		}

		entry := childstats.Entry{
			ChildInvocationID:  reportChildChildID,
			ParentInvocationID: reportChildParentID,
			BuildTool:          reportChildBuildTool,
			HitRate:            hitRate,
			Hits:               reportChildHits,
			Total:              reportChildTotal,
			BenchmarkPhase:     reportChildBenchmarkPhase,
			WrittenAt:          time.Now().UTC(),
		}

		if err := childstats.NewWriter().Write(entry); err != nil {
			return fmt.Errorf("write child stats ledger: %w", err)
		}

		return nil
	},
}

func init() {
	reportChildStatsCmd.Flags().StringVar(&reportChildParentID, "parent-id", "", "Parent invocation ID (required; no-op if empty)")
	reportChildStatsCmd.Flags().StringVar(&reportChildChildID, "child-id", "", "Child invocation ID (required; no-op if empty)")
	reportChildStatsCmd.Flags().StringVar(&reportChildBuildTool, "build-tool", "", "Build tool label (gradle, ccache, xcode, ...)")
	reportChildStatsCmd.Flags().Float32Var(&reportChildHitRate, "hit-rate", 0, "Cache hit rate in [0,1]. If unset and --total > 0, computed from --hits / --total.")
	reportChildStatsCmd.Flags().Int64Var(&reportChildHits, "hits", 0, "Number of cache hits (optional)")
	reportChildStatsCmd.Flags().Int64Var(&reportChildTotal, "total", 0, "Total cacheable calls (optional)")
	reportChildStatsCmd.Flags().StringVar(&reportChildBenchmarkPhase, "benchmark-phase", "", "Benchmark phase label (baseline excludes from mean)")

	RootCmd.AddCommand(reportChildStatsCmd)
}
