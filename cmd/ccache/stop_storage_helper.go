package ccache

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache"
	ccacheanalytics "github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/analytics"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

//nolint:gochecknoglobals
var stopHelperSocketPath string

//nolint:gochecknoglobals
var stopStorageHelperCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Flush final ccache statistics and shut down the storage helper",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		socketPath := stopHelperSocketPath
		if socketPath == "" {
			config, err := ccacheconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
			if err != nil {
				return fmt.Errorf("read ccache config (use --socket to override): %w", err)
			}
			socketPath = config.IPCEndpoint
		}

		if !ccache.IsListening(socketPath) {
			log.NewLogger().TInfof("Storage helper is not running, nothing to stop")

			return nil
		}

		if err := ccache.SendStop(cmd.Context(), socketPath); err != nil {
			return fmt.Errorf("send stop to storage helper: %w", err)
		}

		return nil
	},
}

// collectAndZeroCcacheStats collects and reports ccache stats, then zeros the counters.
// Zeroing only happens after a successful collection and send — stats are never lost silently.
// Errors are logged but do not fail the caller.
func collectAndZeroCcacheStats(ctx context.Context, config ccacheconfig.Config, invocationID, parentID string, downloadedBytes, uploadedBytes int64, logger log.Logger) {
	client, err := ccacheanalytics.NewClient(consts.CcacheAnalyticsServiceEndpoint, config.AuthConfig.TokenInGradleFormat(), logger)
	if err != nil {
		logger.TErrorf("Skipping ccache stats collection because analytics client creation failed: %v", err)

		return
	}

	ccacheanalytics.CollectAndZero(ctx, client, invocationID, parentID, downloadedBytes, uploadedBytes, logger)
}

func init() {
	stopStorageHelperCmd.Flags().StringVar(&stopHelperSocketPath, "socket", "", "Path to the ccache IPC socket (defaults to value from config)")

	storageHelperCmd.AddCommand(stopStorageHelperCmd)
}
