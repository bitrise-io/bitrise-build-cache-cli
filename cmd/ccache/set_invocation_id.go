package ccache

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

var ( //nolint:gochecknoglobals
	setInvocationIDValue      string
	setInvocationIDSocketPath string
)

//nolint:gochecknoglobals
var setInvocationIDCmd = &cobra.Command{
	Use:          "set-invocation-id",
	Short:        "Send an invocation ID to the running storage helper",
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		socketPath := setInvocationIDSocketPath
		if socketPath == "" {
			config, err := ccacheconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
			if err != nil {
				return fmt.Errorf("read ccache config (use --socket to override): %w", err)
			}
			socketPath = config.IPCEndpoint
		}

		if err := ccache.SendInvocationID(socketPath, setInvocationIDValue); err != nil {
			return fmt.Errorf("send invocation ID: %w", err)
		}

		return nil
	},
}

func init() {
	setInvocationIDCmd.Flags().StringVar(&setInvocationIDValue, "id", "", "Invocation ID to send (required)")
	_ = setInvocationIDCmd.MarkFlagRequired("id")
	setInvocationIDCmd.Flags().StringVar(&setInvocationIDSocketPath, "socket", "", "Path to the ccache IPC socket (defaults to value from config)")

	storageHelperCmd.AddCommand(setInvocationIDCmd)
}
