package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/spf13/cobra"
)

// addGradleVerificationReferenceDeps represents the gradle command
var addGradleVerificationReferenceDeps = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "add-reference-deps",
	Short: "Add Bitrise Build Cache plugins to the project (but do not enable it)",
	Long: `Add Bitrise Build Cache plugins to the project (but do not enable it)
This command will:

- Create a ~/.gradle/init.d/bitrise-build-cache.init.gradle.kts file with the necessary configs. This file will be overwritten.
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
	gradleVerification.AddCommand(addGradleVerificationReferenceDeps)
}

func addGradlePluginsFn(logger log.Logger, gradleHomePath string, envProvider func(string) string) error {
	logger.Infof("(i) Checking parameters")

	// Optional configs
	// EndpointURL
	endpointURL := common.SelectCacheEndpointURL(paramRemoteCacheEndpoint, envProvider)
	logger.Infof("(i) Build Cache Endpoint URL: %s", endpointURL)
	logger.Infof("(i) Debug mode and verbose logs: %t", isDebugLogMode)

	// Metadata
	cacheConfigMetadata := common.NewCacheConfigMetadata(os.Getenv, func(name string, v ...string) (string, error) {
		output, err := exec.Command(name, v...).Output()

		return string(output), err
	}, logger)
	logger.Infof("(i) Cache Config Metadata: %+v", cacheConfigMetadata)

	authToken := "placeholder-token"
	prefs := gradleconfig.Preferences{
		IsDependencyOnly:     true,
		IsPushEnabled:        false,
		CacheLevelValidation: gradleconfig.CacheValidationLevelNone,
		IsAnalyticsEnabled:   true,
		IsDebugEnabled:       isDebugLogMode,
	}
	if err := writeGradleInit(logger, gradleHomePath, endpointURL, authToken, cacheConfigMetadata, prefs); err != nil {
		return err
	}

	return nil
}
