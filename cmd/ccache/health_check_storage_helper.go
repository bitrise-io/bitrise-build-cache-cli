package ccache

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/pkg/ccache"
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
		helper, err := ccachepkg.NewStorageHelper(ccachepkg.StorageHelperParams{
			SocketPath: healthCheckSocketPath,
		})
		if err != nil {
			return fmt.Errorf("create storage helper: %w", err)
		}

		if err := helper.HealthCheck(cmd.Context(), ccachepkg.HealthCheckParams{
			Timeout:      healthCheckTimeout,
			PollInterval: healthCheckPollInterval,
		}); err != nil {
			return fmt.Errorf("health check: %w", err)
		}

		return nil
	},
}

func init() {
	healthCheckStorageHelperCmd.Flags().StringVar(&healthCheckSocketPath, "socket", "", "Path to the ccache IPC socket (defaults to value from config)")
	healthCheckStorageHelperCmd.Flags().DurationVar(&healthCheckTimeout, "timeout", 10*time.Second, "How long to wait for the server to become ready")
	healthCheckStorageHelperCmd.Flags().DurationVar(&healthCheckPollInterval, "poll-interval", 100*time.Millisecond, "How often to retry the health check")

	storageHelperCmd.AddCommand(healthCheckStorageHelperCmd)
}
