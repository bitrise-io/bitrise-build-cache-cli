package cmd

import (
	"fmt"
	"os"

	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/spf13/cobra"
)

const gradleHomeNonExpanded = "~/.gradle"

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
	RunE: func(_ *cobra.Command, _ []string) error {
		//
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof("Enable Bitrise Build Cache for Gradle")
		//
		gradleHome, err := pathutil.NewPathModifier().AbsPath(gradleHomeNonExpanded)
		if err != nil {
			return fmt.Errorf("expand Gradle home path (%s), error: %w", gradleHome, err)
		}

		//
		if err := enableForGradleCmdFn(logger, gradleHome, os.Getenv); err != nil {
			return fmt.Errorf("enable Gradle Build Cache: %w", err)
		}

		logger.TInfof("✅ Bitrise Build Cache for Gradle enabled")

		return nil
	},
}

func init() {
	enableForCmd.AddCommand(enableForGradleCmd)
	enableForGradleCmd.Flags().BoolVar(&paramIsGradleMetricsEnabled, "metrics", true, "Gradle Metrics collection enabled/disabled. Used for cache insights.")
	enableForGradleCmd.Flags().BoolVar(&paramIsPushEnabled, "push", true, "Push enabled/disabled. Enabled means the build can also write new entries to the remote cache. Disabled means the build can only read from the remote cache.")
	enableForGradleCmd.Flags().StringVar(&paramValidationLevel, "validation-level", "warning", "Level of cache entry validation for both uploads and downloads. Possible values: none, warning, error")
	enableForGradleCmd.Flags().StringVar(&paramRemoteCacheEndpoint, "remote-cache-endpoint", "", "Remote cache endpoint URL")
}

func enableForGradleCmdFn(logger log.Logger, gradleHomePath string, envProvider func(string) string) error {
	activateForGradleParams.Cache.Enabled = true
	activateForGradleParams.Cache.PushEnabled = paramIsPushEnabled
	activateForGradleParams.Cache.ValidationLevel = paramValidationLevel
	activateForGradleParams.Cache.Endpoint = paramRemoteCacheEndpoint
	activateForGradleParams.Analytics.Enabled = paramIsGradleMetricsEnabled
	activateForGradleParams.TestDistro.Enabled = false

	templateInventory, err := activateForGradleParams.templateInventory(logger, envProvider)
	if err != nil {
		return fmt.Errorf(FmtErrorEnableForGradle, err)
	}

	if err := templateInventory.WriteToGradleInit(
		logger,
		gradleHomePath,
		gradleconfig.DefaultOsProxy(),
		gradleconfig.DefaultTemplateProxy(),
	); err != nil {
		return fmt.Errorf(FmtErrorEnableForGradle, err)
	}

	if err := defaultGradlePropertiesUpdater().updateGradleProps(activateForGradleParams, logger, gradleHomePath); err != nil {
		return fmt.Errorf(FmtErrorEnableForGradle, err)
	}

	return nil
}

//nolint:gochecknoglobals
var (
	FmtErrorEnableForGradle = "adding Gradle plugins failed: %w"
)
