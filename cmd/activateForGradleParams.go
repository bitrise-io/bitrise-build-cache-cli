package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/gradle"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/go-utils/v2/log"
)

//nolint:gochecknoglobals
var (
	errInvalidCacheLevel = errors.New("invalid cache validation level, valid options: none, warning, error")
	errTestDistroAppSlug = errors.New("test distribution plugin was enabled but no BITRISE_APP_SLUG was specified")

	errFmtReadAutConfig            = "read auth config from environment variables: %w"
	errFmtCacheConfigCreation      = "couldn't create cache configuration: %w"
	errFmtTestDistroConfigCreation = "couldn't create test distribution configuration: %w"
	errFmtInvalidValidationLevel   = "invalid validation level: '%s'"
	errFmtGradlePropertiesCheck    = "check if gradle.properties exists at %s, error: %w"
	errFmtGradlePropertyWrite      = "write gradle.properties to %s, error: %w"
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
			ValidationLevel: string(gradleconfig.CacheValidationLevelWarning),
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

func (params ActivateForGradleParams) templateInventory(
	logger log.Logger,
	envProvider func(string) string,
) (gradleconfig.TemplateInventory, error) {
	logger.Infof("(i) Checking parameters")

	commonInventory, err := params.commonTemplateInventory(logger, envProvider)
	if err != nil {
		return gradleconfig.TemplateInventory{}, err
	}

	cacheInventory, err := params.cacheTemplateInventory(logger, envProvider)
	if err != nil {
		return gradleconfig.TemplateInventory{}, fmt.Errorf(errFmtCacheConfigCreation, err)
	}

	analyticsInventory := params.analyticsTemplateInventory(logger)

	testDistroInventory, err := params.testDistroTemplateInventory(logger, envProvider)
	if err != nil {
		return gradleconfig.TemplateInventory{}, fmt.Errorf(errFmtTestDistroConfigCreation, err)
	}

	return gradleconfig.TemplateInventory{
		Common:     commonInventory,
		Cache:      cacheInventory,
		Analytics:  analyticsInventory,
		TestDistro: testDistroInventory,
	}, nil
}

func (params ActivateForGradleParams) commonTemplateInventory(
	logger log.Logger,
	envProvider func(string) string,
) (gradleconfig.PluginCommonTemplateInventory, error) {
	logger.Infof("(i) Debug mode and verbose logs: %t", isDebugLogMode)

	// Required configs
	logger.Infof("(i) Check Auth Config")
	authConfig, err := common.ReadAuthConfigFromEnvironments(envProvider)
	if err != nil {
		return gradleconfig.PluginCommonTemplateInventory{},
			fmt.Errorf(errFmtReadAutConfig, err)
	}
	authToken := authConfig.TokenInGradleFormat()

	cacheConfig := common.NewMetadata(os.Getenv,
		func(name string, v ...string) (string, error) {
			output, err := exec.Command(name, v...).Output()

			return string(output), err
		},
		logger)
	logger.Infof("(i) Cache Config: %+v", cacheConfig)

	return gradleconfig.PluginCommonTemplateInventory{
		AuthToken:  authToken,
		Debug:      isDebugLogMode,
		AppSlug:    cacheConfig.BitriseAppID,
		CIProvider: cacheConfig.CIProvider,
	}, nil
}

func (params ActivateForGradleParams) cacheTemplateInventory(
	logger log.Logger,
	envProvider func(string) string,
) (gradleconfig.CacheTemplateInventory, error) {
	if !params.Cache.JustDependency && !params.Cache.Enabled {
		logger.Infof("(i) Cache plugin usage: %+v", gradleconfig.UsageLevelNone)

		return gradleconfig.CacheTemplateInventory{
			Usage: gradleconfig.UsageLevelNone,
		}, nil
	}

	if params.Cache.JustDependency && !params.Cache.Enabled {
		logger.Infof("(i) Cache plugin usage: %+v", gradleconfig.UsageLevelDependency)

		return gradleconfig.CacheTemplateInventory{
			Usage:   gradleconfig.UsageLevelDependency,
			Version: consts.GradleRemoteBuildCachePluginDepVersion,
		}, nil
	}

	logger.Infof("(i) Cache plugin usage: %+v", gradleconfig.UsageLevelEnabled)

	cacheEndpointURL := common.SelectCacheEndpointURL(params.Cache.Endpoint, envProvider)
	logger.Infof("(i) Build Cache Endpoint URL: %s", cacheEndpointURL)
	logger.Infof("(i) Push new cache entries: %t", params.Cache.PushEnabled)
	logger.Infof("(i) Cache entry validation level: %s", params.Cache.ValidationLevel)

	if params.Cache.ValidationLevel != string(gradleconfig.CacheValidationLevelNone) &&
		params.Cache.ValidationLevel != string(gradleconfig.CacheValidationLevelWarning) &&
		params.Cache.ValidationLevel != string(gradleconfig.CacheValidationLevelError) {
		logger.Errorf(errFmtInvalidValidationLevel, params.Cache.ValidationLevel)

		return gradleconfig.CacheTemplateInventory{}, errInvalidCacheLevel
	}

	return gradleconfig.CacheTemplateInventory{
		Usage:               gradleconfig.UsageLevelDependency,
		Version:             consts.GradleRemoteBuildCachePluginDepVersion,
		EndpointURLWithPort: cacheEndpointURL,
		IsPushEnabled:       params.Cache.PushEnabled,
		ValidationLevel:     params.Cache.ValidationLevel,
	}, nil
}

func (params ActivateForGradleParams) analyticsTemplateInventory(
	logger log.Logger,
) gradleconfig.AnalyticsTemplateInventory {
	if !params.Analytics.JustDependency && !params.Analytics.Enabled {
		logger.Infof("(i) Analytics plugin usage: %+v", gradleconfig.UsageLevelNone)

		return gradleconfig.AnalyticsTemplateInventory{
			Usage: gradleconfig.UsageLevelNone,
		}
	}

	if params.Analytics.JustDependency && !params.Analytics.Enabled {
		logger.Infof("(i) Analytics plugin usage: %+v", gradleconfig.UsageLevelDependency)

		return gradleconfig.AnalyticsTemplateInventory{
			Usage:   gradleconfig.UsageLevelDependency,
			Version: consts.GradleAnalyticsPluginDepVersion,
		}
	}

	logger.Infof("(i) Analytics plugin usage: %+v", gradleconfig.UsageLevelEnabled)

	return gradleconfig.AnalyticsTemplateInventory{
		Usage:        gradleconfig.UsageLevelEnabled,
		Version:      consts.GradleAnalyticsPluginDepVersion,
		Endpoint:     consts.GradleAnalyticsEndpoint,
		Port:         consts.GradleAnalyticsPort,
		HTTPEndpoint: consts.GradleAnalyticsHTTPEndpoint,
	}
}

func (params ActivateForGradleParams) testDistroTemplateInventory(
	logger log.Logger,
	envProvider func(string) string,
) (gradleconfig.TestDistroTemplateInventory, error) {
	if !params.TestDistro.JustDependency && !params.TestDistro.Enabled {
		logger.Infof("(i) Test distribution plugin usage: %+v", gradleconfig.UsageLevelNone)

		return gradleconfig.TestDistroTemplateInventory{
			Usage: gradleconfig.UsageLevelNone,
		}, nil
	}

	if params.TestDistro.JustDependency && !params.TestDistro.Enabled {
		logger.Infof("(i) Test distribution plugin usage: %+v", gradleconfig.UsageLevelDependency)

		return gradleconfig.TestDistroTemplateInventory{
			Usage:   gradleconfig.UsageLevelDependency,
			Version: consts.GradleTestDistributionPluginDepVersion,
		}, nil
	}

	logger.Infof("(i) Test distribution plugin usage: %+v", gradleconfig.UsageLevelEnabled)

	appSlug := envProvider("BITRISE_APP_SLUG")
	if len(appSlug) < 1 {
		return gradleconfig.TestDistroTemplateInventory{}, errTestDistroAppSlug
	}

	logLevel := "warning"
	if isDebugLogMode {
		logLevel = "debug"
	}

	return gradleconfig.TestDistroTemplateInventory{
		Usage:      gradleconfig.UsageLevelEnabled,
		Version:    consts.GradleTestDistributionPluginDepVersion,
		Endpoint:   consts.GradleTestDistributionEndpoint,
		KvEndpoint: consts.GradleTestDistributionKvEndpoint,
		Port:       consts.GradleTestDistributionPort,
		LogLevel:   logLevel,
	}, nil
}
