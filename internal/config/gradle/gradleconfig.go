package gradleconfig

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"text/template"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
)

//go:embed initd.gradle.kts.gotemplate
var gradleTemplateText string

var (
	errAuthTokenNotProvided                = errors.New("AuthToken not provided")
	errEndpointURLNotProvided              = errors.New("EndpointURL not provided")
	errMissingAppSlugWhenTestDistroEnabled = errors.New("AppSlug not provided when TestDistroEnabled")
)

type CacheValidationLevel string

//nolint:gochecknoglobals
var (
	CacheValidationLevelNone    CacheValidationLevel = "none"
	CacheValidationLevelWarning CacheValidationLevel = "warning"
	CacheValidationLevelError   CacheValidationLevel = "error"
)

type UsageLevel string

//nolint:gochecknoglobals
var (
	UsageLevelNone       UsageLevel = "none"
	UsageLevelDependency UsageLevel = "dependency"
	UsageLevelEnabled    UsageLevel = "enabled"
)

type CachePreferences struct {
	Usage                UsageLevel
	EndpointURL          string
	IsPushEnabled        bool
	CacheLevelValidation CacheValidationLevel
	Metadata             common.CacheConfigMetadata
}

type AnalyticsPreferences struct {
	Usage UsageLevel
}

type TestDistroPreferences struct {
	Usage UsageLevel
}

type Preferences struct {
	IsDebugEnabled bool
	AuthToken      string
	AppSlug        string
	Cache          CachePreferences
	Analytics      AnalyticsPreferences
	TestDistro     TestDistroPreferences
}

type CacheTemplateInventory struct {
	Version             string
	Enabled             bool
	Dependency          bool
	EndpointURLWithPort string
	IsPushEnabled       bool
	ValidationLevel     string
	Metadata            common.CacheConfigMetadata
}

type AnalyticsTemplateInventory struct {
	Enabled      bool
	Dependency   bool
	Version      string
	Endpoint     string
	Port         int
	HttpEndpoint string
}

type TestDistroTemplateInventory struct {
	Enabled    bool
	Dependency bool
	Version    string
	Endpoint   string
	KvEndpoint string
	Port       int
	LogLevel   string
}

type TemplateInventory struct {
	AuthToken      string
	IsDebugEnabled bool
	AppSlug        string
	Cache          CacheTemplateInventory
	Analytics      AnalyticsTemplateInventory
	TestDistro     TestDistroTemplateInventory
}

// Generate init.gradle content.
// Recommended to save the content into $HOME/.gradle/init.d/ instead of
// overwriting the $HOME/.gradle/init.gradle file.
func GenerateInitGradle(preferences Preferences) (string, error) {
	// required check
	if len(preferences.AuthToken) < 1 {
		return "", fmt.Errorf("generate init.gradle, error: %w", errAuthTokenNotProvided)
	}

	if len(preferences.Cache.EndpointURL) < 1 && preferences.Cache.Usage == UsageLevelEnabled {
		return "", fmt.Errorf("generate init.gradle, error: %w", errEndpointURLNotProvided)
	}

	if len(preferences.AppSlug) < 1 && preferences.TestDistro.Usage == UsageLevelEnabled {
		return "", fmt.Errorf("generate init.gradle, error: %w", errMissingAppSlugWhenTestDistroEnabled)
	}

	logLevel := "warning"
	if preferences.IsDebugEnabled {
		logLevel = "debug"
	}

	// create inventory
	inventory := TemplateInventory{
		AuthToken:      preferences.AuthToken,
		IsDebugEnabled: preferences.IsDebugEnabled,
		AppSlug:        preferences.AppSlug,
		Cache: CacheTemplateInventory{
			Enabled:             preferences.Cache.Usage == UsageLevelEnabled,
			Dependency:          preferences.Cache.Usage == UsageLevelDependency || preferences.Cache.Usage == UsageLevelEnabled,
			Version:             consts.GradleRemoteBuildCachePluginDepVersion,
			EndpointURLWithPort: preferences.Cache.EndpointURL,
			IsPushEnabled:       preferences.Cache.IsPushEnabled,
			ValidationLevel:     string(preferences.Cache.CacheLevelValidation),
			Metadata:            preferences.Cache.Metadata,
		},
		Analytics: AnalyticsTemplateInventory{
			Enabled:      preferences.Analytics.Usage == UsageLevelEnabled,
			Dependency:   preferences.Analytics.Usage == UsageLevelDependency || preferences.Analytics.Usage == UsageLevelEnabled,
			Version:      consts.GradleAnalyticsPluginDepVersion,
			Endpoint:     consts.GradleAnalyticsEndpoint,
			Port:         consts.GradleAnalyticsPort,
			HttpEndpoint: consts.GradleAnalyticsHTTPEndpoint,
		},
		TestDistro: TestDistroTemplateInventory{
			Enabled:    preferences.TestDistro.Usage == UsageLevelEnabled,
			Dependency: preferences.TestDistro.Usage == UsageLevelDependency || preferences.TestDistro.Usage == UsageLevelEnabled,
			Version:    consts.GradleTestDistributionPluginDepVersion,
			Endpoint:   consts.GradleTestDistributionEndpoint,
			KvEndpoint: consts.GradleTestDistributionKvEndpoint,
			Port:       consts.GradleTestDistributionPort,
			LogLevel:   logLevel,
		},
	}

	tmpl, err := template.New("init.gradle").Parse(gradleTemplateText)
	if err != nil {
		return "", fmt.Errorf("generate init.gradle: invalid template: %w", err)
	}

	resultBuffer := bytes.Buffer{}
	if err = tmpl.Execute(&resultBuffer, inventory); err != nil {
		return "", fmt.Errorf("GenerateInitGradle: %w", err)
	}

	return resultBuffer.String(), nil
}
