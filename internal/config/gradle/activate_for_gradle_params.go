package gradleconfig

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/go-utils/v2/log"
)

const (
	errFmtInvalidCacheLevel        = "invalid cache validation level, valid options: none, warning, error"
	errFmtTestDistroAppSlug        = "test distribution plugin was enabled but no BITRISE_APP_SLUG was specified"
	ErrFmtReadAutConfig            = "read auth config from environment variables: %w"
	errFmtCacheConfigCreation      = "couldn't create cache configuration: %w"
	errFmtTestDistroConfigCreation = "couldn't create test distribution configuration: %w"
	errFmtInvalidValidationLevel   = "invalid validation level: '%s'"
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

type ActivateGradleParams struct {
	Cache      CacheParams
	Analytics  AnalyticsParams
	TestDistro TestDistroParams
}

func DefaultActivateGradleParams() ActivateGradleParams {
	return ActivateGradleParams{
		Cache: CacheParams{
			Enabled:         false,
			JustDependency:  false,
			PushEnabled:     false,
			ValidationLevel: string(CacheValidationLevelWarning),
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

func (params ActivateGradleParams) TemplateInventory(
	logger log.Logger,
	envProvider func(string) string,
	isDebug bool,
) (TemplateInventory, error) {
	logger.Infof("(i) Checking parameters")

	commonInventory, err := params.commonTemplateInventory(logger, envProvider, isDebug)
	if err != nil {
		return TemplateInventory{}, err
	}

	cacheInventory, err := params.cacheTemplateInventory(logger, envProvider)
	if err != nil {
		return TemplateInventory{}, fmt.Errorf(errFmtCacheConfigCreation, err)
	}

	analyticsInventory := params.analyticsTemplateInventory(logger)

	testDistroInventory, err := params.testDistroTemplateInventory(logger, envProvider, isDebug)
	if err != nil {
		return TemplateInventory{}, fmt.Errorf(errFmtTestDistroConfigCreation, err)
	}

	return TemplateInventory{
		Common:     commonInventory,
		Cache:      cacheInventory,
		Analytics:  analyticsInventory,
		TestDistro: testDistroInventory,
	}, nil
}

func (params ActivateGradleParams) commonTemplateInventory(
	logger log.Logger,
	envProvider func(string) string,
	isDebug bool,
) (PluginCommonTemplateInventory, error) {
	logger.Infof("(i) Debug mode and verbose logs: %t", isDebug)

	// Required configs
	logger.Infof("(i) Check Auth Config")
	authConfig, err := common.ReadAuthConfigFromEnvironments(envProvider)
	if err != nil {
		return PluginCommonTemplateInventory{},
			fmt.Errorf(ErrFmtReadAutConfig, err)
	}
	authToken := authConfig.TokenInGradleFormat()

	cacheConfig := common.NewMetadata(envProvider,
		func(name string, v ...string) (string, error) {
			output, err := exec.Command(name, v...).Output()

			return string(output), err
		},
		logger)
	logger.Infof("(i) Cache Config: %+v", cacheConfig)

	return PluginCommonTemplateInventory{
		AuthToken:  authToken,
		Debug:      isDebug,
		AppSlug:    cacheConfig.BitriseAppID,
		CIProvider: cacheConfig.CIProvider,
		Version:    consts.GradleCommonPluginDepVersion,
	}, nil
}

func (params ActivateGradleParams) cacheTemplateInventory(
	logger log.Logger,
	envProvider func(string) string,
) (CacheTemplateInventory, error) {
	if !params.Cache.JustDependency && !params.Cache.Enabled {
		logger.Infof("(i) Cache plugin usage: %+v", UsageLevelNone)

		return CacheTemplateInventory{
			Usage: UsageLevelNone,
		}, nil
	}

	if params.Cache.JustDependency && !params.Cache.Enabled {
		logger.Infof("(i) Cache plugin usage: %+v", UsageLevelDependency)

		return CacheTemplateInventory{
			Usage:   UsageLevelDependency,
			Version: consts.GradleRemoteBuildCachePluginDepVersion,
		}, nil
	}

	logger.Infof("(i) Cache plugin usage: %+v", UsageLevelEnabled)

	cacheEndpointURL := common.SelectCacheEndpointURL(params.Cache.Endpoint, envProvider)
	logger.Infof("(i) Build Cache Endpoint URL: %s", cacheEndpointURL)
	logger.Infof("(i) Push new cache entries: %t", params.Cache.PushEnabled)
	logger.Infof("(i) Cache entry validation level: %s", params.Cache.ValidationLevel)

	if params.Cache.ValidationLevel != string(CacheValidationLevelNone) &&
		params.Cache.ValidationLevel != string(CacheValidationLevelWarning) &&
		params.Cache.ValidationLevel != string(CacheValidationLevelError) {
		logger.Errorf(errFmtInvalidValidationLevel, params.Cache.ValidationLevel)

		return CacheTemplateInventory{}, errors.New(errFmtInvalidCacheLevel)
	}

	return CacheTemplateInventory{
		Usage:               UsageLevelEnabled,
		Version:             consts.GradleRemoteBuildCachePluginDepVersion,
		EndpointURLWithPort: cacheEndpointURL,
		IsPushEnabled:       params.Cache.PushEnabled,
		ValidationLevel:     params.Cache.ValidationLevel,
	}, nil
}

func (params ActivateGradleParams) analyticsTemplateInventory(
	logger log.Logger,
) AnalyticsTemplateInventory {
	if !params.Analytics.JustDependency && !params.Analytics.Enabled {
		logger.Infof("(i) Analytics plugin usage: %+v", UsageLevelNone)

		return AnalyticsTemplateInventory{
			Usage: UsageLevelNone,
		}
	}

	if params.Analytics.JustDependency && !params.Analytics.Enabled {
		logger.Infof("(i) Analytics plugin usage: %+v", UsageLevelDependency)

		return AnalyticsTemplateInventory{
			Usage:   UsageLevelDependency,
			Version: consts.GradleAnalyticsPluginDepVersion,
		}
	}

	logger.Infof("(i) Analytics plugin usage: %+v", UsageLevelEnabled)

	return AnalyticsTemplateInventory{
		Usage:        UsageLevelEnabled,
		Version:      consts.GradleAnalyticsPluginDepVersion,
		Endpoint:     consts.GradleAnalyticsEndpoint,
		Port:         consts.GradleAnalyticsPort,
		HTTPEndpoint: consts.GradleAnalyticsHTTPEndpoint,
	}
}

func (params ActivateGradleParams) testDistroTemplateInventory(
	logger log.Logger,
	envProvider func(string) string,
	isDebug bool,
) (TestDistroTemplateInventory, error) {
	if !params.TestDistro.JustDependency && !params.TestDistro.Enabled {
		logger.Infof("(i) Test distribution plugin usage: %+v", UsageLevelNone)

		return TestDistroTemplateInventory{
			Usage: UsageLevelNone,
		}, nil
	}

	if params.TestDistro.JustDependency && !params.TestDistro.Enabled {
		logger.Infof("(i) Test distribution plugin usage: %+v", UsageLevelDependency)

		return TestDistroTemplateInventory{
			Usage:   UsageLevelDependency,
			Version: consts.GradleTestDistributionPluginDepVersion,
		}, nil
	}

	logger.Infof("(i) Test distribution plugin usage: %+v", UsageLevelEnabled)

	appSlug := envProvider("BITRISE_APP_SLUG")
	if len(appSlug) < 1 {
		return TestDistroTemplateInventory{}, errors.New(errFmtTestDistroAppSlug)
	}

	logLevel := "warning"
	if isDebug {
		logLevel = "debug"
	}

	return TestDistroTemplateInventory{
		Usage:      UsageLevelEnabled,
		Version:    consts.GradleTestDistributionPluginDepVersion,
		Endpoint:   consts.GradleTestDistributionEndpoint,
		KvEndpoint: consts.GradleTestDistributionKvEndpoint,
		Port:       consts.GradleTestDistributionPort,
		LogLevel:   logLevel,
	}, nil
}
