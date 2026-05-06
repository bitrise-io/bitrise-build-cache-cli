package common

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/health"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

//nolint:gochecknoglobals
var (
	healthcheckEndpoint   string
	healthcheckJSONOutput bool
)

//nolint:gochecknoglobals
var healthcheckCmd = &cobra.Command{
	Use:           "healthcheck",
	Short:         "Test connectivity to the Bitrise Build Cache backend",
	Long:          "Calls GetCapabilities on the build cache backend. On success, updates the local health state used by the status command.",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logOpts := []log.LoggerOptions{log.WithDebugLog(IsDebugLogMode)}
		if healthcheckJSONOutput {
			logOpts = append(logOpts, log.WithOutput(cmd.ErrOrStderr()))
		}
		logger := log.NewLogger(logOpts...)
		logger.TInfof("Bitrise Build Cache healthcheck")

		allEnvs := utils.AllEnvs()
		authConfig, err := configcommon.ReadAuthConfigFromEnvironments(allEnvs)
		if err != nil {
			return fmt.Errorf("read auth config: %w", err)
		}

		kvClient, err := CreateKVClient(cmd.Context(), CreateKVClientParams{
			ClientName:  ClientNameGradle,
			AuthConfig:  authConfig,
			Envs:        allEnvs,
			Logger:      logger,
			EndpointURL: healthcheckEndpoint,
			CommandFunc: func(name string, v ...string) (string, error) {
				output, err := exec.Command(name, v...).Output() //nolint:noctx // host metadata collection, no meaningful context to pass

				return string(output), err
			},
			SkipCapabilities: true,
		})
		if err != nil {
			return fmt.Errorf("create build cache client: %w", err)
		}

		if err := kvClient.GetCapabilitiesWithRetry(cmd.Context()); err != nil {
			wrappedErr := fmt.Errorf("build cache backend unreachable: %w", err)
			if healthcheckJSONOutput {
				_ = WriteJSON(cmd.OutOrStdout(), map[string]any{"success": false, "error": wrappedErr.Error()})
			} else {
				logger.TErrorf("%s", wrappedErr)
			}

			return wrappedErr
		}

		if homeDir, err := os.UserHomeDir(); err == nil {
			health.NewTracker(homeDir).RecordSuccess()
		}

		logger.TInfof("✅ Build cache backend reachable")

		if healthcheckJSONOutput {
			return WriteJSON(cmd.OutOrStdout(), map[string]any{"success": true, "error": nil})
		}

		return nil
	},
}

func init() {
	RootCmd.AddCommand(healthcheckCmd)
	healthcheckCmd.Flags().StringVar(&healthcheckEndpoint, "cache-endpoint", "", "Build cache endpoint URL (defaults to env BITRISE_BUILD_CACHE_ENDPOINT)")
	healthcheckCmd.Flags().BoolVar(&healthcheckJSONOutput, "json", false, "Emit machine-readable JSON to stdout instead of human-readable output")
}
