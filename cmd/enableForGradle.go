package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/stringmerge"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/spf13/cobra"
)

const gradleHomeNonExpanded = "~/.gradle"

var paramIsGradleMetricsEnabled bool //nolint:gochecknoglobals

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
	enableForGradleCmd.Flags().BoolVar(&paramIsGradleMetricsEnabled, "metrics", true, "Gradle Metrics collection enabled/disabled")
}

func enableForGradleCmdFn(logger log.Logger, gradleHomePath string, envProvider func(string) string) error {
	logger.Infof("(i) Checking parameters")

	// Required configs
	logger.Infof("(i) Check Auth Config")
	authConfig, err := common.ReadAuthConfigFromEnvironments(envProvider)
	if err != nil {
		return fmt.Errorf("read auth config from environment variables: %w", err)
	}
	authToken := authConfig.TokenInGradleFormat()

	// Optional configs
	// EndpointURL
	endpointURL := common.SelectEndpointURL(envProvider("BITRISE_BUILD_CACHE_ENDPOINT"), envProvider)
	logger.Infof("(i) Build Cache Endpoint URL: %s", endpointURL)
	logger.Infof("(i) paramIsGradleMetricsEnabled: %t", paramIsGradleMetricsEnabled)
	// Metadata
	cacheConfigMetadata := common.NewCacheConfigMetadata(os.Getenv)
	logger.Infof("(i) Cache Config Metadata: %+v", cacheConfigMetadata)

	logger.Infof("(i) Ensure ~/.gradle and ~/.gradle/init.d directories exist")
	gradleInitDPath := filepath.Join(gradleHomePath, "init.d")
	err = os.MkdirAll(gradleInitDPath, 0755) //nolint:gomnd,mnd
	if err != nil {
		return fmt.Errorf("ensure ~/.gradle/init.d exists: %w", err)
	}

	logger.Infof("(i) Generate ~/.gradle/init.d/bitrise-build-cache.init.gradle.kts")
	initGradleContent, err := gradleconfig.GenerateInitGradle(endpointURL, authToken, paramIsGradleMetricsEnabled, cacheConfigMetadata)
	if err != nil {
		return fmt.Errorf("generate bitrise-build-cache.init.gradle.kts: %w", err)
	}

	logger.Infof("(i) Write ~/.gradle/init.d/bitrise-build-cache.init.gradle.kts")
	{
		initGradlePath := filepath.Join(gradleInitDPath, "bitrise-build-cache.init.gradle.kts")
		err = os.WriteFile(initGradlePath, []byte(initGradleContent), 0755) //nolint:gosec,gomnd,mnd
		if err != nil {
			return fmt.Errorf("write bitrise-build-cache.init.gradle.kts to %s, error: %w", initGradlePath, err)
		}
	}

	logger.Infof("(i) Write ~/.gradle/gradle.properties")
	{
		gradlePropertiesPath := filepath.Join(gradleHomePath, "gradle.properties")
		currentGradlePropsFileContent, isGradlePropsExists, err := readFileIfExists(gradlePropertiesPath)
		if err != nil {
			return fmt.Errorf("check if gradle.properties exists at %s, error: %w", gradlePropertiesPath, err)
		}
		logger.Debugf("isGradlePropsExists: %t", isGradlePropsExists)

		gradlePropertiesContent := stringmerge.ChangeContentInBlock(
			currentGradlePropsFileContent,
			"# [start] generated-by-bitrise-build-cache",
			"# [end] generated-by-bitrise-build-cache",
			"org.gradle.caching=true",
		)

		err = os.WriteFile(gradlePropertiesPath, []byte(gradlePropertiesContent), 0755) //nolint:gosec,gomnd,mnd
		if err != nil {
			return fmt.Errorf("write gradle.properties to %s, error: %w", gradlePropertiesPath, err)
		}
	}

	return nil
}
