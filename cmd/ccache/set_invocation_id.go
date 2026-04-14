package ccache

import (
	"fmt"

	"github.com/spf13/cobra"

	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/pkg/ccache"
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
		helper, err := ccachepkg.NewStorageHelper(ccachepkg.StorageHelperParams{
			SocketPath: setInvocationIDSocketPath,
		})
		if err != nil {
			return fmt.Errorf("create storage helper: %w", err)
		}

		if err := helper.SetInvocationID(cmd.Context(), setInvocationIDParentID, setInvocationIDChildID); err != nil {
			return fmt.Errorf("set invocation ID: %w", err)
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
