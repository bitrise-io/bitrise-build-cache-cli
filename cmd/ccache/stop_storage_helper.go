package ccache

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache"
	ccacheanalytics "github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/analytics"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

//nolint:gochecknoglobals
var (
	stopHelperSocketPath   string
	stopHelperInvocationID string
)

//nolint:gochecknoglobals
var stopStorageHelperCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Flush final ccache statistics and shut down the storage helper",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()

		config, err := ccacheconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
		if err != nil {
			return fmt.Errorf("read ccache config (use --socket to override): %w", err)
		}

		socketPath := stopHelperSocketPath
		if socketPath == "" {
			socketPath = config.IPCEndpoint
		}

		if !ccache.IsListening(socketPath) {
			logger.TInfof("Storage helper is not running, nothing to stop")

			return nil
		}

		parentInvocationID := resolveParentInvocationID(stopHelperInvocationID, utils.AllEnvs())

		ccacheInvocationID := uuid.New().String()

		dl, ul, err := ccache.SendGetSessionStats(cmd.Context(), socketPath)
		if err != nil {
			logger.TWarnf("Failed to get session stats from storage helper: %v", err)
		}

		if err := ccache.SendStop(cmd.Context(), socketPath); err != nil {
			return fmt.Errorf("send stop to storage helper: %w", err)
		}

		collectAndZeroCcacheStats(cmd.Context(), config, ccacheInvocationID, parentInvocationID, dl, ul, logger)
		if parentInvocationID != "" {
			registerInvocationRelation(config, parentInvocationID, ccacheInvocationID, logger)
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

// resolveParentInvocationID returns the parent invocation ID to use when reporting analytics.
// The flag value takes priority; if empty, falls back to BITRISE_INVOCATION_ID from the environment.
func resolveParentInvocationID(flagValue string, envs map[string]string) string {
	if flagValue != "" {
		return flagValue
	}

	return envs["BITRISE_INVOCATION_ID"]
}

func init() {
	stopStorageHelperCmd.Flags().StringVar(&stopHelperSocketPath, "socket", "", "Path to the ccache IPC socket (defaults to value from config)")
	stopStorageHelperCmd.Flags().StringVar(&stopHelperInvocationID, "invocation-id", "", "Parent invocation ID used when reporting analytics (omit if there is no parent)")

	storageHelperCmd.AddCommand(stopStorageHelperCmd)
}
