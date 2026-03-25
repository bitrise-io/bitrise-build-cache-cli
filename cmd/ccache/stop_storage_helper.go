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
func zeroCcacheStats(ctx context.Context, logger log.Logger) {
	ccachePath, err := exec.LookPath("ccache")
	if err != nil {
		return
	}

	if err := exec.CommandContext(ctx, ccachePath, "-z").Run(); err != nil { //nolint:gosec
		logger.TErrorf("Failed to reset ccache stats: %v", err)
	}
}

// collectCcacheStats runs ccache --print-stats, parses the output, and sends a CcacheInvocation
// combining the stats with accumulated byte counts to the analytics backend.
// Returns an error if any step fails; the caller decides whether to zero stats.
func collectCcacheStats(ctx context.Context, config ccacheconfig.Config, invocationID, parentID string, downloadedBytes, uploadedBytes int64, logger log.Logger) error {
	ccachePath, lookErr := exec.LookPath("ccache")
	if lookErr != nil {
		logger.TInfof("ccache not found on PATH, skipping stats collection")

		return nil //nolint:nilerr // missing ccache binary is not an error; stats collection is best-effort
	}

	statsData, err := exec.CommandContext(ctx, ccachePath, "--print-stats", "--format=json").Output() //nolint:gosec
	if err != nil {
		return fmt.Errorf("collect ccache stats: %w", err)
	}

	stats, err := ccacheanalytics.ParseCcacheStats(statsData)
	if err != nil {
		return fmt.Errorf("parse ccache stats: %w", err)
	}

	client, err := ccacheanalytics.NewClient(consts.CcacheAnalyticsServiceEndpoint, config.AuthConfig.TokenInGradleFormat(), logger)
	if err != nil {
		return fmt.Errorf("create analytics client: %w", err)
	}

	inv := ccacheanalytics.NewCcacheInvocation(invocationID, parentID, time.Now(), stats, downloadedBytes, uploadedBytes)
	if err := client.PutCcacheInvocation(*inv); err != nil {
		return fmt.Errorf("send ccache stats (invocationID=%s): %w", invocationID, err)
	}

	return nil
}

// collectAndZeroCcacheStats collects and reports ccache stats, then zeros the counters.
// Zeroing only happens after a successful collection and send — stats are never lost silently.
// Errors are logged but do not fail the caller.
func collectAndZeroCcacheStats(ctx context.Context, config ccacheconfig.Config, invocationID, parentID string, downloadedBytes, uploadedBytes int64, logger log.Logger) {
	if err := collectCcacheStats(ctx, config, invocationID, parentID, downloadedBytes, uploadedBytes, logger); err != nil {
		logger.TErrorf("Skipping ccache stats reset because collection/send failed: %v", err)

		return
	}

	zeroCcacheStats(ctx, logger)
}

func init() {
	stopStorageHelperCmd.Flags().StringVar(&stopHelperSocketPath, "socket", "", "Path to the ccache IPC socket (defaults to value from config)")

	storageHelperCmd.AddCommand(stopStorageHelperCmd)
}
