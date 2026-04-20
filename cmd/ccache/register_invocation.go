package ccache

import (
	"fmt"

	"github.com/spf13/cobra"

	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/ccache"
)

//nolint:gochecknoglobals
var (
	registerInvocationID        string
	registerInvocationBuildTool string
)

//nolint:gochecknoglobals
var registerInvocationCmd = &cobra.Command{
	Use:          "register-invocation",
	Short:        "Register an invocation with the analytics backend",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		inv, err := ccachepkg.NewInvocationRegistry(ccachepkg.InvocationRegistryParams{})
		if err != nil {
			return fmt.Errorf("create invocation registry: %w", err)
		}

		if err := inv.RegisterMultiplatformInvocation(cmd.Context(), ccachepkg.RegisterInvocationParams{
			InvocationID: registerInvocationID,
			BuildTool:    registerInvocationBuildTool,
		}); err != nil {
			return fmt.Errorf("register invocation: %w", err)
		}

		return nil
	},
}

func init() {
	registerInvocationCmd.Flags().StringVar(&registerInvocationID, "invocation-id", "", "Invocation ID to register (required)")
	_ = registerInvocationCmd.MarkFlagRequired("invocation-id")
	registerInvocationCmd.Flags().StringVar(&registerInvocationBuildTool, "build-tool", "multiplatform", "Build tool label for the invocation")

	ccacheCmd.AddCommand(registerInvocationCmd)
}
