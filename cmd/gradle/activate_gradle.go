package gradle

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/gradle"
	mirrorsconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/gradle/mirrors"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/permhint"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
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

		allEnvs := utils.AllEnvs()

		p, err := paths.Default()
		if err != nil {
			return fmt.Errorf("resolve home dir: %w", err)
		}

		gradleHome := p.GradleHome(allEnvs[paths.GradleUserHomeEnvKey])

		if merr := mirrorsconfig.MigratePrebootInitScript(logger, utils.DefaultOsProxy{}, p.GradleHome(""), gradleHome); merr != nil {
			logger.Warnf("Could not relocate preboot Gradle mirrors init script: %s", merr)
		}

		if cliPath, exeErr := os.Executable(); exeErr == nil {
			activateGradleParams.CLIPath = cliPath
		}

		if err := gradleconfig.Activate(
			logger,
			gradleHome,
			allEnvs,
			common.IsDebugLogMode,
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
			activateGradleParams,
		); err != nil {
			permhint.PrintIfApplicable(logger, err)

			return fmt.Errorf("activate plugins for Gradle: %w", err)
		}

		configcommon.LogBenchmarkSummary(logger, []string{configcommon.BuildToolGradle})

		// Best-effort: sidecar write failure must not fail the activate.
		if home, homeErr := os.UserHomeDir(); homeErr == nil {
			initFile := filepath.Join(gradleHome, "init.d", "bitrise-build-cache.init.gradle.kts")
			if err := gradleconfig.WriteSidecar(home, gradleconfig.Sidecar{
				InitScriptPath:   initFile,
				CacheEnabled:     activateGradleParams.Cache.Enabled,
				CachePushEnabled: activateGradleParams.Cache.PushEnabled,
				AnalyticsEnabled: activateGradleParams.Analytics.Enabled,
			}); err != nil {
				logger.Debugf("gradle sidecar write failed (non-fatal): %s", err)
			}
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
	ActivateGradleCmd.Flags().IntVar(&activateGradleParams.TestDistro.TestSearchDepth, "test-distribution-search-depth", activateGradleParams.TestDistro.TestSearchDepth, "Search depth for test distribution when trying to find test tasks not listed in the invocation.")
}

// ErrFmtFailedToUpdateProps is re-exported for backward compatibility with existing tests.
var ErrFmtFailedToUpdateProps = gradleconfig.ErrFmtFailedToUpdateProps //nolint:gochecknoglobals

// ActivateGradleCmdFn is a backward-compatible wrapper around gradleconfig.Activate
// that reads IsDebugLogMode from the global flag. Prefer gradleconfig.Activate directly.
func ActivateGradleCmdFn(
	logger log.Logger,
	gradleHomePath string,
	envProvider map[string]string,
	templateInventoryProvider func(log.Logger, map[string]string, bool, configcommon.BenchmarkPhaseProvider) (gradleconfig.TemplateInventory, error),
	templateWriter func(gradleconfig.TemplateInventory, string) error,
	updater gradleconfig.GradlePropertiesUpdater,
	params gradleconfig.ActivateGradleParams,
) error {
	return gradleconfig.Activate(logger, gradleHomePath, envProvider, common.IsDebugLogMode, templateInventoryProvider, templateWriter, updater, params) //nolint:wrapcheck // thin wrapper, error context added by caller
}
