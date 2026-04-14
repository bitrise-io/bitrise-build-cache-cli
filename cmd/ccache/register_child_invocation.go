package ccache

import (
	"fmt"

	"github.com/spf13/cobra"

	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/pkg/ccache"
)

//nolint:gochecknoglobals
var (
	registerChildParentID  string
	registerChildChildID   string
	registerChildBuildTool string
)

//nolint:gochecknoglobals
var registerChildInvocationCmd = &cobra.Command{
	Use:          "register-child-invocation",
	Short:        "Register a parent→child relationship between two invocation IDs",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		inv, err := ccachepkg.NewInvocationRegistry(ccachepkg.InvocationRegistryParams{})
		if err != nil {
			return fmt.Errorf("create invocation registry: %w", err)
		}

		if err := inv.RegisterRelation(cmd.Context(), ccachepkg.RegisterRelationParams{
			ParentID:  registerChildParentID,
			ChildID:   registerChildChildID,
			BuildTool: registerChildBuildTool,
		}); err != nil {
			return fmt.Errorf("register invocation relation: %w", err)
		}

		return nil
	},
}

func init() {
	registerChildInvocationCmd.Flags().StringVar(&registerChildParentID, "parent-id", "", "Parent invocation ID (required)")
	_ = registerChildInvocationCmd.MarkFlagRequired("parent-id")
	registerChildInvocationCmd.Flags().StringVar(&registerChildChildID, "child-id", "", "Child invocation ID (required)")
	_ = registerChildInvocationCmd.MarkFlagRequired("child-id")
	registerChildInvocationCmd.Flags().StringVar(&registerChildBuildTool, "build-tool", "ccache", "Build tool label for the child invocation (e.g. ccache, gradle)")

	ccacheCmd.AddCommand(registerChildInvocationCmd)
}
