package ccache

import (
	"fmt"

	"github.com/spf13/cobra"

	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/ccache"
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
		helper, err := ccachepkg.NewStorageHelper(ccachepkg.StorageHelperParams{
			SocketPath:         stopHelperSocketPath,
			ParentInvocationID: stopHelperInvocationID,
		})
		if err != nil {
			return fmt.Errorf("create storage helper: %w", err)
		}

		if err := helper.Stop(cmd.Context()); err != nil {
			return fmt.Errorf("stop storage helper: %w", err)
		}

		return nil
	},
}

func init() {
	stopStorageHelperCmd.Flags().StringVar(&stopHelperSocketPath, "socket", "", "Path to the ccache IPC socket (defaults to value from config)")
	stopStorageHelperCmd.Flags().StringVar(&stopHelperInvocationID, "invocation-id", "", "Parent invocation ID used when reporting analytics (omit if there is no parent)")

	storageHelperCmd.AddCommand(stopStorageHelperCmd)
}
