package ccache

import (
	"context"
	"fmt"
	"os/exec"
	"time"

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

// zeroCcacheStats resets ccache's internal counters so each invocation starts from a clean slate.
// If ccache is not on PATH, this is a no-op.
func zeroCcacheStats(logger log.Logger) {
	ccachePath, err := exec.LookPath("ccache")
	if err != nil {
		return
	}

	if err := exec.CommandContext(context.Background(), ccachePath, "-z").Run(); err != nil { //nolint:gosec
		logger.TErrorf("Failed to reset ccache stats: %v", err)
	}
}

// collectAndSendCcacheStats runs ccache --print-stats, parses the output, and sends a CcacheInvocation
// combining the stats with accumulated byte counts to the analytics backend.
// Errors are logged but do not fail the caller — stats reporting is best-effort.
func collectAndSendCcacheStats(ctx context.Context, config ccacheconfig.Config, invocationID, parentID string, downloadedBytes, uploadedBytes int64, logger log.Logger) {
	ccachePath, err := exec.LookPath("ccache")
	if err != nil {
		logger.TInfof("ccache not found on PATH, skipping stats collection")

		return
	}

	statsData, err := exec.CommandContext(ctx, ccachePath, "--print-stats", "--format=json").Output() //nolint:gosec
	if err != nil {
		logger.TErrorf("Failed to collect ccache stats: %v", err)

		return
	}

	stats, err := ccacheanalytics.ParseCcacheStats(statsData)
	if err != nil {
		logger.TErrorf("Failed to parse ccache stats: %v", err)

		return
	}

	client, err := ccacheanalytics.NewClient(consts.CcacheAnalyticsServiceEndpoint, config.AuthConfig.TokenInGradleFormat(), logger)
	if err != nil {
		logger.TErrorf("Failed to create analytics client for ccache stats: %v", err)

		return
	}

	inv := ccacheanalytics.NewCcacheInvocation(invocationID, parentID, time.Now(), stats, downloadedBytes, uploadedBytes)
	if err := client.PutCcacheInvocation(*inv); err != nil {
		logger.TErrorf("Failed to send ccache stats (invocationID=%s): %v", invocationID, err)
	}
}

func init() {
	stopStorageHelperCmd.Flags().StringVar(&stopHelperSocketPath, "socket", "", "Path to the ccache IPC socket (defaults to value from config)")

	storageHelperCmd.AddCommand(stopStorageHelperCmd)
}
