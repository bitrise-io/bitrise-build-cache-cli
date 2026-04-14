package ccache

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/common"
	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/pkg/ccache"
)

//nolint:gochecknoglobals
var (
	initialInvocationID string

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
		Use:          "start",
		Short:        "Start Xcelerate's ccache storage helper",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			helper, err := ccachepkg.NewStorageHelper(ccachepkg.StorageHelperParams{
				InvocationID: initialInvocationID,
				DebugLogging: common.IsDebugLogMode,
			})
			if err != nil {
				return fmt.Errorf("create storage helper: %w", err)
			}

			if err := helper.Start(cmd.Context()); err != nil {
				return fmt.Errorf("run storage helper: %w", err)
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

	common.RootCmd.AddCommand(ccacheCmd)
	ccacheCmd.AddCommand(storageHelperCmd)
	storageHelperCmd.AddCommand(startStorageHelperCmd)
}
