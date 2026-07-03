package gradle

import (
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const (
	FmtErrorEnableForGradle = "adding Gradle plugins failed: %w"
)

//nolint:gochecknoglobals
var (
	paramIsGradleMetricsEnabled bool
	paramIsPushEnabled          bool
	paramValidationLevel        string
	paramRemoteCacheEndpoint    string
)

// enableForGradleCmd represents the gradle command
var enableForGradleCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "gradle",
	Short: "Enable Bitrise Build Cache for Gradle",
	Long: `Enable Bitrise Build Cache for Gradle.
This command will:

- Create a ~/.gradle/init.d/bitrise-build-cache.init.gradle.kts file with the necessary configs. This file will be overwritten.
- Create a ~/.gradle/gradle.properties file with org.gradle.caching=true

The gradle.properties file will be created if it doesn't exist.
If it already exists a "# [start/end] generated-by-bitrise-build-cache" block will be added to the end of the file.
If the "# [start/end] generated-by-bitrise-build-cache" block is already present in the file then only the block's content will be modified.
`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		//
		logger := log.NewLogger()
		logger.EnableDebugLog(common.IsDebugLogMode)
		logger.TInfof("Enable Bitrise Build Cache for Gradle")
		//
		allEnvs := utils.AllEnvs()

		p, err := paths.Default()
		if err != nil {
			return fmt.Errorf("resolve home dir: %w", err)
		}

		gradleHome := p.GradleHome(allEnvs[paths.GradleUserHomeEnvKey])

		//
		if err := EnableForGradleCmdFn(logger, gradleHome, allEnvs); err != nil {
			return fmt.Errorf("enable Gradle Build Cache: %w", err)
		}

		logger.TInfof("✅ Bitrise Build Cache for Gradle enabled")

		return nil
	},
}

func init() {
	common.EnableForCmd.AddCommand(enableForGradleCmd)
	enableForGradleCmd.Flags().BoolVar(&paramIsGradleMetricsEnabled, "metrics", true, "Gradle Metrics collection enabled/disabled. Used for cache insights.")
	enableForGradleCmd.Flags().BoolVar(&paramIsPushEnabled, "push", true, "Push enabled/disabled. Enabled means the build can also write new entries to the remote cache. Disabled means the build can only read from the remote cache.")
	enableForGradleCmd.Flags().StringVar(&paramValidationLevel, "validation-level", "warning", "Level of cache entry validation for both uploads and downloads. Possible values: none, warning, error")
	enableForGradleCmd.Flags().StringVar(&paramRemoteCacheEndpoint, "remote-cache-endpoint", "", "Remote cache endpoint URL")
}

func EnableForGradleCmdFn(logger log.Logger, gradleHomePath string, envProvider map[string]string) error {
	activateGradleParams.Cache.Enabled = true
	activateGradleParams.Cache.PushEnabled = paramIsPushEnabled
	activateGradleParams.Cache.ValidationLevel = paramValidationLevel
	activateGradleParams.Cache.Endpoint = paramRemoteCacheEndpoint
	activateGradleParams.Analytics.Enabled = paramIsGradleMetricsEnabled
	activateGradleParams.TestDistro.Enabled = false

	if cliPath, exeErr := os.Executable(); exeErr == nil {
		activateGradleParams.CLIPath = cliPath
	}

	authConfig, _, err := configcommon.ResolveAuthConfig(envProvider)
	if err != nil {
		return fmt.Errorf(FmtErrorEnableForGradle, fmt.Errorf(gradleconfig.ErrFmtReadAuthConfig, err))
	}

	benchmarkClient := configcommon.NewBenchmarkPhaseClient(consts.BitriseWebsiteBaseURL, authConfig, logger)

	templateInventory, err := activateGradleParams.TemplateInventory(logger, envProvider, common.IsDebugLogMode, benchmarkClient)
	if err != nil {
		return fmt.Errorf(FmtErrorEnableForGradle, err)
	}

	if err := templateInventory.WriteToGradleInit(
		logger,
		gradleHomePath,
		utils.DefaultOsProxy{},
		gradleconfig.GradleTemplateProxy(),
	); err != nil {
		return fmt.Errorf(FmtErrorEnableForGradle, err)
	}

	if err := gradleconfig.DefaultGradlePropertiesUpdater().UpdateGradleProps(activateGradleParams, logger, gradleHomePath); err != nil {
		return fmt.Errorf(FmtErrorEnableForGradle, err)
	}

	return nil
}
