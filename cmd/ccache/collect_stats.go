package ccache

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
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
			if collectStatsInvocationID == "" {
				return fmt.Errorf("--invocation-id is required")
			}

			config, err := ccacheconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
			if err != nil {
				return fmt.Errorf("read ccache config: %w", err)
			}

			dl, ul := collectStatsDownloadBytes, collectStatsUploadBytes

			// If the storage helper is running, prefer its session byte counts over the flag values.
			if ccache.IsListening(config.IPCEndpoint) {
				sessionDL, sessionUL, err := ccache.SendGetSessionStats(cmd.Context(), config.IPCEndpoint)
				if err != nil {
					log.NewLogger().TWarnf("Failed to get session stats from storage helper: %v", err)
				} else {
					dl, ul = sessionDL, sessionUL
				}
			}

			logger := log.NewLogger()
			collectAndZeroCcacheStats(
				cmd.Context(),
				config,
				collectStatsInvocationID,
				collectStatsParentID,
				dl,
				ul,
				logger,
			)

			return nil
		},
	}
)

func init() {
	collectStatsCmd.Flags().StringVar(&collectStatsInvocationID, "invocation-id", "", "Invocation ID to report stats under (required)")
	collectStatsCmd.Flags().StringVar(&collectStatsParentID, "parent-id", "", "Parent invocation ID")
	collectStatsCmd.Flags().Int64Var(&collectStatsDownloadBytes, "downloaded-bytes", 0, "Bytes downloaded from cache during this invocation (overridden by session state if helper is running)")
	collectStatsCmd.Flags().Int64Var(&collectStatsUploadBytes, "uploaded-bytes", 0, "Bytes uploaded to cache during this invocation (overridden by session state if helper is running)")

	storageHelperCmd.AddCommand(collectStatsCmd)
}
