package gradleconfig

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/envexport"
)

const (
	errFmtInvalidCacheLevel        = "invalid cache validation level, valid options: none, warning, error"
	errFmtTestDistroAppSlug        = "test distribution plugin was enabled but no BITRISE_APP_SLUG was specified"
	ErrFmtReadAuthConfig           = "read auth config from environment variables: %w"
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
	Enabled         bool
	JustDependency  bool
	ShardSize       int
	TestSearchDepth int
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
			Enabled:         false,
			JustDependency:  false,
			ShardSize:       50,
			TestSearchDepth: 3,
		},
	}
}

func (params ActivateGradleParams) TemplateInventory(
	logger log.Logger,
	envs map[string]string,
	isDebug bool,
	benchmarkProvider common.BenchmarkPhaseProvider,
) (TemplateInventory, error) {
	logger.Infof("(i) Checking parameters")

	// Read auth config and metadata upfront
	logger.Infof("(i) Check Auth Config")
	authConfig, err := common.ReadAuthConfigFromEnvironments(envs)
	if err != nil {
		return TemplateInventory{}, fmt.Errorf(ErrFmtReadAuthConfig, err)
	}

	metadata := common.NewMetadata(envs,
		func(name string, v ...string) (string, error) {
			output, err := exec.Command(name, v...).Output() //nolint:noctx

			return string(output), err
		},
		logger)
	logger.Infof("(i) Cache Config: %+v", metadata)

	// Check benchmark phase and override params if needed (only on CI)
	if metadata.CIProvider != "" && benchmarkProvider != nil {
		logger.Debugf("Checking benchmark phase...CI Provider: %s", metadata.CIProvider)
		ApplyBenchmarkPhase(&params, logger, benchmarkProvider, metadata, envexport.New(envs, logger))
	}

	commonInventory := params.commonTemplateInventory(authConfig, metadata, isDebug)

	cacheInventory, err := params.cacheTemplateInventory(logger, envs)
	if err != nil {
		return TemplateInventory{}, fmt.Errorf(errFmtCacheConfigCreation, err)
	}

	analyticsInventory := params.analyticsTemplateInventory(logger)

	testDistroInventory := params.testDistroTemplateInventory(logger, isDebug)

	return TemplateInventory{
		Common:     commonInventory,
		Cache:      cacheInventory,
		Analytics:  analyticsInventory,
		TestDistro: testDistroInventory,
	}, nil
}

func (params ActivateGradleParams) commonTemplateInventory(
	authConfig common.CacheAuthConfig,
	metadata common.CacheConfigMetadata,
	isDebug bool,
) PluginCommonTemplateInventory {
	return PluginCommonTemplateInventory{
		AuthToken:  authConfig.TokenInGradleFormat(),
		Debug:      isDebug,
		AppSlug:    metadata.BitriseAppID,
		CIProvider: metadata.CIProvider,
		Version:    consts.GradleCommonPluginDepVersion,
	}
}

func (params ActivateGradleParams) cacheTemplateInventory(
	logger log.Logger,
	envs map[string]string,
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

	cacheEndpointURL := common.SelectCacheEndpointURL(params.Cache.Endpoint, envs)
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
		GRPCEndpoint: consts.GradleAnalyticsGRPCEndpoint,
	}
}

func (params ActivateGradleParams) testDistroTemplateInventory(
	logger log.Logger,
	isDebug bool,
) TestDistroTemplateInventory {
	if !params.TestDistro.JustDependency && !params.TestDistro.Enabled {
		logger.Infof("(i) Test distribution plugin usage: %+v", UsageLevelNone)

		return TestDistroTemplateInventory{
			Usage: UsageLevelNone,
		}
	}

	if params.TestDistro.JustDependency && !params.TestDistro.Enabled {
		logger.Infof("(i) Test distribution plugin usage: %+v", UsageLevelDependency)

		return TestDistroTemplateInventory{
			Usage:   UsageLevelDependency,
			Version: consts.GradleTestDistributionPluginDepVersion,
		}
	}

	logger.Infof("(i) Test distribution plugin usage: %+v", UsageLevelEnabled)

	logLevel := "warning"
	if isDebug {
		logLevel = "debug"
	}

	return TestDistroTemplateInventory{
		Usage:           UsageLevelEnabled,
		Version:         consts.GradleTestDistributionPluginDepVersion,
		Endpoint:        consts.GradleTestDistributionEndpoint,
		KvEndpoint:      consts.GradleTestDistributionKvEndpoint,
		Port:            consts.GradleTestDistributionPort,
		LogLevel:        logLevel,
		ShardSize:       params.TestDistro.ShardSize,
		TestSearchDepth: params.TestDistro.TestSearchDepth,
	}
}
