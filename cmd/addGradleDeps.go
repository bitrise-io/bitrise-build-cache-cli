package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/spf13/cobra"
)

// addGradleDeps represents the gradle command
var addGradleDeps = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "add-gradle-deps",
	Short: "Add Bitrise Build Cache plugins to the project (but do not enable it)",
	Long: `Add Bitrise Build Cache plugins to the project (but do not enable it)
This command will:

- Create a ~/.gradle/init.d/bitrise-build-cache.init.gradle.kts file with the necessary configs. This file will be overwritten.

The gradle.properties file will be created if it doesn't exist.
If it already exists a "# [start/end] generated-by-bitrise-build-cache" block will be added to the end of the file.
If the "# [start/end] generated-by-bitrise-build-cache" block is already present in the file then only the block's content will be modified.
`,
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		//
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof("Add Bitrise Build Cache for Gradle plugins")
		//
		gradleHome, err := pathutil.NewPathModifier().AbsPath(gradleHomeNonExpanded)
		if err != nil {
			return fmt.Errorf("expand Gradle home path (%s), error: %w", gradleHome, err)
		}

		//
		if err := addGradlePluginsFn(logger, gradleHome, os.Getenv); err != nil {
			return fmt.Errorf("enable Gradle Build Cache: %w", err)
		}

		logger.TInfof("✅ Bitrise Build Cache plugins added")

		return nil
	},
}

func init() {
	enableForCmd.AddCommand(addGradleDeps)
}

func addGradlePluginsFn(logger log.Logger, gradleHomePath string, envProvider func(string) string) error {
	logger.Infof("(i) Checking parameters")

	// Optional configs
	// EndpointURL
	endpointURL := common.SelectEndpointURL(paramRemoteCacheEndpoint, envProvider)
	logger.Infof("(i) Build Cache Endpoint URL: %s", endpointURL)
	logger.Infof("(i) Debug mode and verbose logs: %t", isDebugLogMode)

	// Metadata
	cacheConfigMetadata := common.NewCacheConfigMetadata(os.Getenv)
	logger.Infof("(i) Cache Config Metadata: %+v", cacheConfigMetadata)

	logger.Infof("(i) Ensure ~/.gradle and ~/.gradle/init.d directories exist")
	gradleInitDPath := filepath.Join(gradleHomePath, "init.d")
	err := os.MkdirAll(gradleInitDPath, 0755) //nolint:gomnd,mnd
	if err != nil {
		return fmt.Errorf("ensure ~/.gradle/init.d exists: %w", err)
	}

	logger.Infof("(i) Generate ~/.gradle/init.d/bitrise-build-cache.init.gradle.kts")
	prefs := gradleconfig.Preferences{
		IsOfflineMode:        true,
		IsPushEnabled:        false,
		CacheLevelValidation: gradleconfig.CacheValidationLevelNone,
		IsAnalyticsEnabled:   true,
		IsDebugEnabled:       isDebugLogMode,
	}

	authToken := "placeholder-token"
	initGradleContent, err := gradleconfig.GenerateInitGradle(endpointURL, authToken, prefs, cacheConfigMetadata)
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

	return nil
}
