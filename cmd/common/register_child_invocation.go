package common

import (
	"fmt"

	"github.com/spf13/cobra"

	pkgcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/common"
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
		inv, err := pkgcommon.NewInvocationRegistry(pkgcommon.InvocationRegistryParams{})
		if err != nil {
			return fmt.Errorf("create invocation registry: %w", err)
		}

		if err := inv.RegisterRelation(cmd.Context(), pkgcommon.RegisterRelationParams{
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

	RootCmd.AddCommand(registerChildInvocationCmd)
}
