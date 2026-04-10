package ccache

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/analytics/multiplatform"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
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
		config, err := ccacheconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
		if err != nil {
			return fmt.Errorf("read ccache config: %w", err)
		}

		logger := log.NewLogger()
		envs := utils.AllEnvs()

		client, err := multiplatform.NewClient(consts.CcacheAnalyticsServiceEndpoint, config.AuthConfig.TokenInGradleFormat(), logger)
		if err != nil {
			return fmt.Errorf("create analytics client: %w", err)
		}

		metadata := common.NewMetadata(envs, func(name string, args ...string) (string, error) {
			out, err := exec.CommandContext(cmd.Context(), name, args...).Output() //nolint:gosec

			return string(out), err
		}, logger)

		inv := multiplatform.NewInvocation(multiplatform.InvocationRunStats{
			InvocationID:   registerInvocationID,
			InvocationDate: time.Now(),
			BuildTool:      registerInvocationBuildTool,
		}, config.AuthConfig, metadata)

		if err := client.PutInvocation(*inv); err != nil {
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
