package common

import (
	"fmt"

	"github.com/spf13/cobra"

	pkgcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/common"
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
		inv, err := pkgcommon.NewInvocationRegistry(pkgcommon.InvocationRegistryParams{})
		if err != nil {
			return fmt.Errorf("create invocation registry: %w", err)
		}

		if err := inv.RegisterMultiplatformInvocation(cmd.Context(), pkgcommon.RegisterInvocationParams{
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

	RootCmd.AddCommand(registerInvocationCmd)
}
