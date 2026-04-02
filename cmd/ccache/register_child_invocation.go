package ccache

import (
	"fmt"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/analytics/multiplatform"
	ccacheanalytics "github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache/analytics"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

//nolint:gochecknoglobals
var (
	registerChildParentID string
	registerChildChildID  string
	registerChildBuildTool string
)

//nolint:gochecknoglobals
var registerChildInvocationCmd = &cobra.Command{
	Use:          "register-child-invocation",
	Short:        "Register a parent→child relationship between two invocation IDs",
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		config, err := ccacheconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
		if err != nil {
			return fmt.Errorf("read ccache config: %w", err)
		}

		logger := log.NewLogger()

		client, err := ccacheanalytics.NewClient(consts.CcacheAnalyticsServiceEndpoint, config.AuthConfig.TokenInGradleFormat(), logger)
		if err != nil {
			return fmt.Errorf("create analytics client: %w", err)
		}

		rel := multiplatform.InvocationRelation{
			ParentInvocationID: registerChildParentID,
			ChildInvocationID:  registerChildChildID,
			InvocationDate:     time.Now(),
			BuildTool:          registerChildBuildTool,
		}

		if err := client.PutInvocationRelation(rel); err != nil {
			return fmt.Errorf("register child invocation: %w", err)
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
