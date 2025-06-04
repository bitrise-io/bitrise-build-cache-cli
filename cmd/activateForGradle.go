package cmd

import (
    "fmt"
    "os"
    "os/exec"
    "path/filepath"

    "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
    gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
    "github.com/bitrise-io/bitrise-build-cache-cli/internal/stringmerge"
    "github.com/bitrise-io/go-utils/v2/log"
    "github.com/bitrise-io/go-utils/v2/pathutil"
    "github.com/spf13/cobra"
)

type CacheParams struct {
    Enabled         bool
    JustDependency  bool
    PushEnabled     bool
    ValidationLevel string
    Endpoint        string
}

type AnalyticsParams struct {
    Enabled        bool
    JustDependency bool
}

type TestDistroParams struct {
    Enabled        bool
    JustDependency bool
}

type ActivateForGradleParams struct {
    Cache      CacheParams
    Analytics  AnalyticsParams
    TestDistro TestDistroParams
}

//nolint:gochecknoglobals
var activateForGradleParams = DefaultActivateForGradleParams()

func DefaultActivateForGradleParams() ActivateForGradleParams {
    return ActivateForGradleParams{
        Cache: CacheParams{
            Enabled:         false,
            JustDependency:  false,
            PushEnabled:     false,
            ValidationLevel: "warning",
        },
        Analytics: AnalyticsParams{
            Enabled:        true,
            JustDependency: false,
        },
        TestDistro: TestDistroParams{
            Enabled:        false,
            JustDependency: false,
        },
    }
}

// activateForGradleCmd represents the `gradle` subcommand under `activate`
var activateForGradleCmd = &cobra.Command{ //nolint:gochecknoglobals
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
    RunE: func(_ *cobra.Command, _ []string) error {
        logger := log.NewLogger()
        logger.EnableDebugLog(isDebugLogMode)
        logger.TInfof("Activate Bitrise plugins for Gradle")

        gradleHome, err := pathutil.NewPathModifier().AbsPath(gradleHomeNonExpanded)
        if err != nil {
            return fmt.Errorf("expand Gradle home path (%s), error: %w", gradleHome, err)
        }

        if err := activateForGradleCmdFn(logger, gradleHome, os.Getenv); err != nil {
            return fmt.Errorf("activate plugins for Gradle: %w", err)
        }

        logger.TInfof("✅ Bitrise plugins activated")

        return nil
    },
}

func init() {
    activateCmd.AddCommand(activateForGradleCmd)
    activateForGradleCmd.Flags().BoolVar(&activateForGradleParams.Cache.Enabled, "cache", activateForGradleParams.Cache.Enabled, "Activate cache plugin. Will override cache-dep.")
    activateForGradleCmd.Flags().BoolVar(&activateForGradleParams.Cache.JustDependency, "cache-dep", activateForGradleParams.Cache.JustDependency, "Add cache plugin as a dependency only.")
    activateForGradleCmd.Flags().BoolVar(&activateForGradleParams.Cache.PushEnabled, "cache-push", activateForGradleParams.Cache.PushEnabled, "Push enabled/disabled. Enabled means the build can also write new entries to the remote cache. Disabled means the build can only read from the remote cache.")
    activateForGradleCmd.Flags().StringVar(&activateForGradleParams.Cache.ValidationLevel, "cache-validation", activateForGradleParams.Cache.ValidationLevel, "Level of cache entry validation for both uploads and downloads. Possible values: none, warning, error")
    activateForGradleCmd.Flags().StringVar(&activateForGradleParams.Cache.Endpoint, "cache-endpoint", activateForGradleParams.Cache.Endpoint, "The endpoint can be manually provided here for caching operations.")

    activateForGradleCmd.Flags().BoolVar(&activateForGradleParams.Analytics.Enabled, "analytics", activateForGradleParams.Analytics.Enabled, "Activate analytics plugin. Will override analytics-dep.")
    activateForGradleCmd.Flags().BoolVar(&activateForGradleParams.Analytics.JustDependency, "analytics-dep", activateForGradleParams.Analytics.JustDependency, "Add analytics plugin as a dependency only.")

    activateForGradleCmd.Flags().BoolVar(&activateForGradleParams.TestDistro.Enabled, "test-distribution", activateForGradleParams.TestDistro.Enabled, "Activate test distribution plugin for the provided app slug. Will override test-distribution-dep.")
    activateForGradleCmd.Flags().BoolVar(&activateForGradleParams.TestDistro.JustDependency, "test-distribution-dep", activateForGradleParams.TestDistro.JustDependency, "Add test distribution plugin as a dependency only.")
}

func activateForGradleCmdFn(logger log.Logger, gradleHomePath string, envProvider func(string) string) error {
    logger.Infof("(i) Checking parameters")
    logger.Infof("(i) Debug mode and verbose logs: %t", isDebugLogMode)

    // Required configs
    logger.Infof("(i) Check Auth Config")
    authConfig, err := common.ReadAuthConfigFromEnvironments(envProvider)
    if err != nil {
        return fmt.Errorf("read auth config from environment variables: %w", err)
    }
    authToken := authConfig.TokenInGradleFormat()

    appSlug := envProvider("BITRISE_APP_SLUG")

    cacheUsageLevel := gradleconfig.UsageLevelNone
    if activateForGradleParams.Cache.JustDependency {
        cacheUsageLevel = gradleconfig.UsageLevelDependency
    }
    if activateForGradleParams.Cache.Enabled {
        cacheUsageLevel = gradleconfig.UsageLevelEnabled
    }
    logger.Infof("(i) Cache plugin usage: %+v", cacheUsageLevel)

    cacheEndpointURL := ""
    cacheConfigMetadata := common.CacheConfigMetadata{}
    if cacheUsageLevel == gradleconfig.UsageLevelEnabled {
        // Optional configs
        // EndpointURL
        cacheEndpointURL = common.SelectCacheEndpointURL(activateForGradleParams.Cache.Endpoint, envProvider)
        logger.Infof("(i) Build Cache Endpoint URL: %s", cacheEndpointURL)
        logger.Infof("(i) Push new cache entries: %t", activateForGradleParams.Cache.PushEnabled)
        logger.Infof("(i) Cache entry validation level: %s", activateForGradleParams.Cache.ValidationLevel)

        if activateForGradleParams.Cache.ValidationLevel != string(gradleconfig.CacheValidationLevelNone) &&
            activateForGradleParams.Cache.ValidationLevel != string(gradleconfig.CacheValidationLevelWarning) &&
            activateForGradleParams.Cache.ValidationLevel != string(gradleconfig.CacheValidationLevelError) {
            logger.Errorf("Invalid validation level: '%s'", activateForGradleParams.Cache.ValidationLevel)

            return errInvalidCacheLevel
        }
        // Metadata
        cacheConfigMetadata = common.NewCacheConfigMetadata(envProvider,
            func(name string, v ...string) (string, error) {
                output, err := exec.Command(name, v...).Output()

                return string(output), err
            }, logger)
        logger.Infof("(i) Cache Config Metadata: %+v", cacheConfigMetadata)
    }

    analyticsUsageLevel := gradleconfig.UsageLevelNone
    if activateForGradleParams.Analytics.JustDependency {
        analyticsUsageLevel = gradleconfig.UsageLevelDependency
    }
    if activateForGradleParams.Analytics.Enabled {
        analyticsUsageLevel = gradleconfig.UsageLevelEnabled
    }
    logger.Infof("(i) Analytics plugin usage: %+v", analyticsUsageLevel)

    testDistroUsageLevel := gradleconfig.UsageLevelNone
    if activateForGradleParams.TestDistro.JustDependency {
        testDistroUsageLevel = gradleconfig.UsageLevelDependency
    }
    if activateForGradleParams.TestDistro.Enabled {
        testDistroUsageLevel = gradleconfig.UsageLevelEnabled
    }
    logger.Infof("(i) Test distribution plugin usage: %+v", testDistroUsageLevel)

    prefs := gradleconfig.Preferences{
        IsDebugEnabled: isDebugLogMode,
        AuthToken:      authToken,
        AppSlug:        appSlug,
        Cache: gradleconfig.CachePreferences{
            Usage:                cacheUsageLevel,
            EndpointURL:          cacheEndpointURL,
            IsPushEnabled:        activateForGradleParams.Cache.PushEnabled,
            CacheLevelValidation: gradleconfig.CacheValidationLevel(activateForGradleParams.Cache.ValidationLevel),
            Metadata:             cacheConfigMetadata,
        },
        Analytics: gradleconfig.AnalyticsPreferences{
            Usage: analyticsUsageLevel,
        },
        TestDistro: gradleconfig.TestDistroPreferences{
            Usage: testDistroUsageLevel,
        },
    }

    if err := writeGradleInit(logger, gradleHomePath, prefs); err != nil {
        return err
    }

    if prefs.Cache.Usage == gradleconfig.UsageLevelEnabled {
        logger.Infof("(i) Write ~/.gradle/gradle.properties")

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
