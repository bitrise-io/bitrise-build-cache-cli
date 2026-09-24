package gradleconfig

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/bitrise-io/go-utils/v2/log"

	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/envexport"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const (
	errFmtInvalidCacheLevel        = "invalid cache validation level, valid options: none, warning, error"
	errFmtTestDistroAppSlug        = "test distribution plugin was enabled but no BITRISE_APP_SLUG was specified"
	ErrFmtReadAuthConfig           = "resolve auth config: %w"
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

	CLIPath string
}

func DefaultActivateGradleParams() ActivateGradleParams {
	return ActivateGradleParams{
		Cache: CacheParams{
			Enabled:         false,
			JustDependency:  false,
			PushEnabled:     true,
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

func NormalizeParams(params *ActivateGradleParams) {
	if params.Cache.PushEnabled {
		params.Cache.Enabled = true
	}
}

// resolveProjectMode reads the machine-wide mode; failures fall back to
// ModeAlways so template rendering never blocks on a machine-config error.
func resolveProjectMode(osProxy utils.OsProxy, logger log.Logger) machineconfig.Mode {
	p, err := paths.Default()
	if err != nil {
		if logger != nil {
			logger.Debugf("Could not resolve home dir for machine config, defaulting to always: %s", err)
		}

		return machineconfig.ModeAlways
	}

	current, err := machineconfig.Read(osProxy, p, logger)
	if err != nil {
		if logger != nil {
			logger.Warnf("Could not read machine config, defaulting to always: %s", err)
		}

		return machineconfig.ModeAlways
	}

	return machineconfig.ResolvedProjectMode(current)
}

func (params ActivateGradleParams) TemplateInventory(
	ctx context.Context,
	logger log.Logger,
	envs map[string]string,
	isDebug bool,
	benchmarkProvider common.BenchmarkPhaseProvider,
	osProxy utils.OsProxy,
) (TemplateInventory, error) {
	NormalizeParams(&params)

	logger.Infof("(i) Checking parameters")

	// Read auth config and metadata upfront
	logger.Infof("(i) Check Auth Config")
	resolver := live.Default(nil)

	authConfig, authOrigin, err := resolver.Resolve(ctx, envs)
	if err != nil {
		return TemplateInventory{}, fmt.Errorf(ErrFmtReadAuthConfig, err)
	}

	username, _ := resolver.ResolveUsername(envs)
	metadata := common.NewMetadata(envs, username,
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

	projectMode := resolveProjectMode(osProxy, logger)

	commonInventory := params.commonTemplateInventory(authConfig, authOrigin, metadata, isDebug, projectMode)

	cacheInventory, err := params.cacheTemplateInventory(logger, envs)
	if err != nil {
		return TemplateInventory{}, fmt.Errorf(errFmtCacheConfigCreation, err)
	}

	analyticsInventory := params.analyticsTemplateInventory(logger)

	testDistroInventory := params.testDistroTemplateInventory(logger, isDebug)

	return TemplateInventory{
		Common:                 commonInventory,
		Cache:                  cacheInventory,
		Analytics:              analyticsInventory,
		TestDistro:             testDistroInventory,
		GradlePluginsMirrorURL: GradlePluginsMirrorURL(envs),
	}, nil
}

func (params ActivateGradleParams) commonTemplateInventory(
	authConfig authpkg.Credential,
	authOrigin authpkg.Origin,
	metadata common.CacheConfigMetadata,
	isDebug bool,
	projectMode machineconfig.Mode,
) PluginCommonTemplateInventory {
	cliPath := params.CLIPath
	if cliPath == "" {
		cliPath = "bitrise-build-cache"
	}

	return PluginCommonTemplateInventory{
		AuthToken:   authpkg.GradleToken(authConfig, authOrigin),
		Debug:       isDebug,
		AppSlug:     metadata.BitriseAppID,
		CIProvider:  metadata.CIProvider,
		Version:     consts.GradleCommonPluginDepVersion,
		CLIPath:     cliPath,
		ProjectMode: string(projectMode),
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
