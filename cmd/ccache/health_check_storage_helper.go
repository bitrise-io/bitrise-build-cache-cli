package ccache

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

//nolint:gochecknoglobals
var (
	healthCheckSocketPath   string
	healthCheckTimeout      time.Duration
	healthCheckPollInterval time.Duration
)

//nolint:gochecknoglobals
var healthCheckStorageHelperCmd = &cobra.Command{
	Use:          "health-check",
	Short:        "Poll the storage helper until it is ready or the timeout expires",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		socketPath := healthCheckSocketPath
		if socketPath == "" {
			config, err := ccacheconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
			if err != nil {
				return fmt.Errorf("read ccache config (use --socket to override): %w", err)
			}

			socketPath = config.IPCEndpoint
		}

		deadline := time.Now().Add(healthCheckTimeout)
		for {
			if err := ccache.SendHealthCheck(cmd.Context(), socketPath); err == nil {
				return nil
			}

			if time.Now().After(deadline) {
				return fmt.Errorf("storage helper did not become ready within %s", healthCheckTimeout)
			}

			select {
			case <-cmd.Context().Done():
				return cmd.Context().Err()
			case <-time.After(healthCheckPollInterval):
			}
		}
	},
}

func init() {
	healthCheckStorageHelperCmd.Flags().StringVar(&healthCheckSocketPath, "socket", "", "Path to the ccache IPC socket (defaults to value from config)")
	healthCheckStorageHelperCmd.Flags().DurationVar(&healthCheckTimeout, "timeout", 10*time.Second, "How long to wait for the server to become ready")
	healthCheckStorageHelperCmd.Flags().DurationVar(&healthCheckPollInterval, "poll-interval", 100*time.Millisecond, "How often to retry the health check")

	storageHelperCmd.AddCommand(healthCheckStorageHelperCmd)
}
