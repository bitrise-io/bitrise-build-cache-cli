package gradle

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/common"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

const (
	ErrFmtFailedToUpdateProps = `failed to update gradle.properties: %w"`
)

// ActivateGradleCmd represents the `gradle` subcommand under `activate`
var ActivateGradleCmd = &cobra.Command{ //nolint:gochecknoglobals
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
		logger.EnableDebugLog(common.IsDebugLogMode)
		logger.TInfof("Activate Bitrise plugins for Gradle")

		gradleHome, err := pathutil.NewPathModifier().AbsPath(gradleHomeNonExpanded)
		if err != nil {
			return fmt.Errorf("expand Gradle home path (%s), error: %w", gradleHome, err)
		}

		allEnvs := utils.AllEnvs()

		if err := ActivateGradleCmdFn(
			logger,
			gradleHome,
			allEnvs,
			activateGradleParams.TemplateInventory,
			func(
				inventory gradleconfig.TemplateInventory,
				path string,
			) error {
				return inventory.WriteToGradleInit(
					logger,
					path,
					utils.DefaultOsProxy{},
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
	common.ActivateCmd.AddCommand(ActivateGradleCmd)
	ActivateGradleCmd.Flags().BoolVar(&activateGradleParams.Cache.Enabled, "cache", activateGradleParams.Cache.Enabled, "Activate cache plugin. Will override cache-dep.")
	ActivateGradleCmd.Flags().BoolVar(&activateGradleParams.Cache.JustDependency, "cache-dep", activateGradleParams.Cache.JustDependency, "Add cache plugin as a dependency only.")
	ActivateGradleCmd.Flags().BoolVar(&activateGradleParams.Cache.PushEnabled, "cache-push", activateGradleParams.Cache.PushEnabled, "Push enabled/disabled. Enabled means the build can also write new entries to the remote cache. Disabled means the build can only read from the remote cache.")
	ActivateGradleCmd.Flags().StringVar(&activateGradleParams.Cache.ValidationLevel, "cache-validation", activateGradleParams.Cache.ValidationLevel, "Level of cache entry validation for both uploads and downloads. Possible values: none, warning, error")
	ActivateGradleCmd.Flags().StringVar(&activateGradleParams.Cache.Endpoint, "cache-endpoint", activateGradleParams.Cache.Endpoint, "The endpoint can be manually provided here for caching operations.")

	ActivateGradleCmd.Flags().BoolVar(&activateGradleParams.Analytics.Enabled, "analytics", activateGradleParams.Analytics.Enabled, "Activate analytics plugin. Will override analytics-dep.")
	ActivateGradleCmd.Flags().BoolVar(&activateGradleParams.Analytics.JustDependency, "analytics-dep", activateGradleParams.Analytics.JustDependency, "Add analytics plugin as a dependency only.")

	ActivateGradleCmd.Flags().BoolVar(&activateGradleParams.TestDistro.Enabled, "test-distribution", activateGradleParams.TestDistro.Enabled, "Activate test distribution plugin for the provided app slug. Will override test-distribution-dep.")
	ActivateGradleCmd.Flags().BoolVar(&activateGradleParams.TestDistro.JustDependency, "test-distribution-dep", activateGradleParams.TestDistro.JustDependency, "Add test distribution plugin as a dependency only.")
	ActivateGradleCmd.Flags().IntVar(&activateGradleParams.TestDistro.ShardSize, "test-distribution-shard-size", activateGradleParams.TestDistro.ShardSize, "Shard size for test distribution plugin.")
}

func ActivateGradleCmdFn(
	logger log.Logger,
	gradleHomePath string,
	envProvider map[string]string,
	templateInventoryProvider func(log.Logger, map[string]string, bool) (gradleconfig.TemplateInventory, error),
	templateWriter func(gradleconfig.TemplateInventory, string) error,
	updater gradleconfig.GradlePropertiesUpdater,
) error {
	templateInventory, err := templateInventoryProvider(logger, envProvider, common.IsDebugLogMode)
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
		return fmt.Errorf(ErrFmtFailedToUpdateProps, err)
	}

	return nil
}
