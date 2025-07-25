package cmd

import (
	"fmt"
	"os"

	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/spf13/cobra"
)

const (
	errFmtFailedToUpdateProps = `failed to update gradle.properties: %w"`
)

// activateGradleCmd represents the `gradle` subcommand under `activate`
var activateGradleCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "gradle",
	Short: "Activate Bitrise Plugins for Gradle",
	Long: `Activate Bitrise Plugins for Gradle.
This command will:

- Create a ~/.gradle/init.d/bitrise-build-cache.init.gradle.kts file with the necessary configs. This file will be overwritten.
- Create a ~/.gradle/gradle.properties file with org.gradle.caching=true when adding the caching plugin.

The gradle.properties file will be created if it doesn't exist.
If it already exists a "# [start/end] generated-by-bitrise-build-cache" block will be added to the end of the file.
If the "# [start/end] generated-by-bitrise-build-cache" block is already present in the file then only the block's content will be modified.
`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof("Activate Bitrise plugins for Gradle")

		gradleHome, err := pathutil.NewPathModifier().AbsPath(gradleHomeNonExpanded)
		if err != nil {
			return fmt.Errorf("expand Gradle home path (%s), error: %w", gradleHome, err)
		}

		if err := getPlugins(cmd.Context(), logger, os.Getenv); err != nil {
			return fmt.Errorf("failed to fetch plugins: %w", err)
		}

		if err := activateGradleCmdFn(
			logger,
			gradleHome,
			os.Getenv,
			activateGradleParams.TemplateInventory,
			func(
				inventory gradleconfig.TemplateInventory,
				path string,
			) error {
				return inventory.WriteToGradleInit(
					logger,
					path,
					utils.DefaultOsProxy(),
					gradleconfig.GradleTemplateProxy(),
				)
			},
			gradleconfig.DefaultGradlePropertiesUpdater(),
		); err != nil {
			return fmt.Errorf("activate plugins for Gradle: %w", err)
		}

		logger.TInfof("✅ Bitrise plugins activated")

		return nil
	},
}

//nolint:gochecknoglobals
var activateGradleParams = gradleconfig.DefaultActivateGradleParams()

func init() {
	activateCmd.AddCommand(activateGradleCmd)
	activateGradleCmd.Flags().BoolVar(&activateGradleParams.Cache.Enabled, "cache", activateGradleParams.Cache.Enabled, "Activate cache plugin. Will override cache-dep.")
	activateGradleCmd.Flags().BoolVar(&activateGradleParams.Cache.JustDependency, "cache-dep", activateGradleParams.Cache.JustDependency, "Add cache plugin as a dependency only.")
	activateGradleCmd.Flags().BoolVar(&activateGradleParams.Cache.PushEnabled, "cache-push", activateGradleParams.Cache.PushEnabled, "Push enabled/disabled. Enabled means the build can also write new entries to the remote cache. Disabled means the build can only read from the remote cache.")
	activateGradleCmd.Flags().StringVar(&activateGradleParams.Cache.ValidationLevel, "cache-validation", activateGradleParams.Cache.ValidationLevel, "Level of cache entry validation for both uploads and downloads. Possible values: none, warning, error")
	activateGradleCmd.Flags().StringVar(&activateGradleParams.Cache.Endpoint, "cache-endpoint", activateGradleParams.Cache.Endpoint, "The endpoint can be manually provided here for caching operations.")

	activateGradleCmd.Flags().BoolVar(&activateGradleParams.Analytics.Enabled, "analytics", activateGradleParams.Analytics.Enabled, "Activate analytics plugin. Will override analytics-dep.")
	activateGradleCmd.Flags().BoolVar(&activateGradleParams.Analytics.JustDependency, "analytics-dep", activateGradleParams.Analytics.JustDependency, "Add analytics plugin as a dependency only.")

	activateGradleCmd.Flags().BoolVar(&activateGradleParams.TestDistro.Enabled, "test-distribution", activateGradleParams.TestDistro.Enabled, "Activate test distribution plugin for the provided app slug. Will override test-distribution-dep.")
	activateGradleCmd.Flags().BoolVar(&activateGradleParams.TestDistro.JustDependency, "test-distribution-dep", activateGradleParams.TestDistro.JustDependency, "Add test distribution plugin as a dependency only.")
}

func activateGradleCmdFn(
	logger log.Logger,
	gradleHomePath string,
	envProvider func(string) string,
	templateInventoryProvider func(log.Logger, func(string) string, bool) (gradleconfig.TemplateInventory, error),
	templateWriter func(gradleconfig.TemplateInventory, string) error,
	updater gradleconfig.GradlePropertiesUpdater,
) error {
	templateInventory, err := templateInventoryProvider(logger, envProvider, isDebugLogMode)
	if err != nil {
		return err
	}

	if err := templateWriter(
		templateInventory,
		gradleHomePath,
	); err != nil {
		return err
	}

	if err := updater.UpdateGradleProps(activateGradleParams, logger, gradleHomePath); err != nil {
		return fmt.Errorf(errFmtFailedToUpdateProps, err)
	}

	return nil
}
