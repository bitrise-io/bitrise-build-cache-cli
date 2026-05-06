package ccache

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/cmd/common"
	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/ccache"
)

//nolint:gochecknoglobals
var (
	initialInvocationID string
	startNoIdleTimeout  bool
	startJSONOutput     bool

	ccacheCmd = &cobra.Command{
		Use:          "ccache",
		Short:        "Ccache related commands",
		SilenceUsage: true,
	}

	storageHelperCmd = &cobra.Command{
		Use:          "storage-helper",
		Short:        "Ccache storage helper",
		SilenceUsage: true,
	}

	startStorageHelperCmd = &cobra.Command{
		Use:           "start",
		Short:         "Start Xcelerate's ccache storage helper",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			helper, err := ccachepkg.NewStorageHelper(ccachepkg.StorageHelperParams{
				InvocationID:  initialInvocationID,
				DebugLogging:  common.IsDebugLogMode,
				NoIdleTimeout: startNoIdleTimeout,
			})
			if err != nil {
				wrappedErr := fmt.Errorf("create storage helper: %w", err)
				if startJSONOutput {
					_ = common.WriteJSON(cmd.OutOrStdout(), map[string]any{"success": false, "error": wrappedErr.Error()})
				}

				return wrappedErr
			}

			if err := helper.Start(cmd.Context()); err != nil {
				wrappedErr := fmt.Errorf("run storage helper: %w", err)
				if startJSONOutput {
					_ = common.WriteJSON(cmd.OutOrStdout(), map[string]any{"success": false, "error": wrappedErr.Error()})
				}

				return wrappedErr
			}

			if startJSONOutput {
				return common.WriteJSON(cmd.OutOrStdout(), map[string]any{"success": true, "error": nil})
			}

			return nil
		},
	}
)

func init() {
	startStorageHelperCmd.Flags().StringVar(
		&initialInvocationID,
		"invocation-id",
		uuid.NewString(),
		"Invocation ID to be used in the proxy",
	)
	startStorageHelperCmd.Flags().BoolVar(
		&startNoIdleTimeout,
		"no-idle-timeout",
		false,
		"Disable the idle-shutdown timer; the helper runs until explicitly stopped (for desktop-app use)",
	)
	startStorageHelperCmd.Flags().BoolVar(
		&startJSONOutput,
		"json",
		false,
		"Emit machine-readable JSON to stdout on exit",
	)

	common.RootCmd.AddCommand(ccacheCmd)
	ccacheCmd.AddCommand(storageHelperCmd)
	storageHelperCmd.AddCommand(startStorageHelperCmd)
}
