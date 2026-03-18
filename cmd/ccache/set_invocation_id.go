package ccache

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

//nolint:gochecknoglobals
var (
	setInvocationIDParentID   string
	setInvocationIDChildID    string
	setInvocationIDSocketPath string
)

//nolint:gochecknoglobals
var setInvocationIDCmd = &cobra.Command{
	Use:          "set-invocation-id",
	Short:        "Send a parent→child invocation ID pair to the running storage helper",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		socketPath := setInvocationIDSocketPath
		if socketPath == "" {
			config, err := ccacheconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
			if err != nil {
				return fmt.Errorf("read ccache config (use --socket to override): %w", err)
			}
			socketPath = config.IPCEndpoint
		}

		if err := ccache.SendInvocationID(cmd.Context(), socketPath, setInvocationIDParentID, setInvocationIDChildID); err != nil {
			return fmt.Errorf("send invocation ID: %w", err)
		}

		return nil
	},
}

func init() {
	setInvocationIDCmd.Flags().StringVar(&setInvocationIDParentID, "parent-id", "", "Parent invocation ID (required)")
	_ = setInvocationIDCmd.MarkFlagRequired("parent-id")
	setInvocationIDCmd.Flags().StringVar(&setInvocationIDChildID, "child-id", "", "Child invocation ID (required)")
	_ = setInvocationIDCmd.MarkFlagRequired("child-id")
	setInvocationIDCmd.Flags().StringVar(&setInvocationIDSocketPath, "socket", "", "Path to the ccache IPC socket (defaults to value from config)")

	storageHelperCmd.AddCommand(setInvocationIDCmd)
}
