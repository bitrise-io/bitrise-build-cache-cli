package ccache

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/ccache"
)

//nolint:gochecknoglobals
var (
	stopHelperSocketPath string
	stopJSONOutput       bool
)

//nolint:gochecknoglobals
var stopStorageHelperCmd = &cobra.Command{
	Use:           "stop",
	Short:         "Shut down the storage helper process",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		helper, err := ccachepkg.NewStorageHelper(ccachepkg.StorageHelperParams{
			SocketPath: stopHelperSocketPath,
		})
		if err != nil {
			wrappedErr := fmt.Errorf("create storage helper: %w", err)
			if stopJSONOutput {
				_ = common.WriteJSON(cmd.OutOrStdout(), map[string]any{"success": false, "error": wrappedErr.Error()})
			}

			return wrappedErr
		}

		if err := helper.Stop(cmd.Context()); err != nil {
			wrappedErr := fmt.Errorf("stop storage helper: %w", err)
			if stopJSONOutput {
				_ = common.WriteJSON(cmd.OutOrStdout(), map[string]any{"success": false, "error": wrappedErr.Error()})
			}

			return wrappedErr
		}

		if stopJSONOutput {
			return common.WriteJSON(cmd.OutOrStdout(), map[string]any{"success": true, "error": nil})
		}

		return nil
	},
}

func init() {
	stopStorageHelperCmd.Flags().StringVar(&stopHelperSocketPath, "socket", "", "Path to the ccache IPC socket (defaults to value from config)")
	stopStorageHelperCmd.Flags().BoolVar(&stopJSONOutput, "json", false, "Emit machine-readable JSON to stdout on completion")

	storageHelperCmd.AddCommand(stopStorageHelperCmd)
}
