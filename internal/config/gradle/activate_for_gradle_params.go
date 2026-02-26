package gradleconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

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
	if metadata.CIProvider != "" {
		benchmarkClient := common.NewBenchmarkPhaseClient(consts.BitriseWebsiteBaseURL, authConfig, logger)
		applyBenchmarkPhase(&params, logger, benchmarkClient, metadata, envs)
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

func applyBenchmarkPhase(
	params *ActivateGradleParams,
	logger log.Logger,
	benchmarkProvider common.BenchmarkPhaseProvider,
	metadata common.CacheConfigMetadata,
	envs map[string]string,
) {
	phase, err := benchmarkProvider.GetGradleBenchmarkPhase(metadata)
	if err != nil {
		logger.Warnf("Failed to fetch benchmark phase, using configured flags: %v", err)

		return
	}

	if phase == "" {
		return
	}

	logger.Infof("(i) Benchmark phase: %s", phase)
	exporter := envexport.New(envs, logger)
	exporter.Export("BITRISE_BUILD_CACHE_BENCHMARK_PHASE", phase)
	exporter.ExportToShellRC("Bitrise Benchmark Phase", "export BITRISE_BUILD_CACHE_BENCHMARK_PHASE="+phase)
	writeBenchmarkPhaseFile(phase, logger)

	switch phase {
	case common.BenchmarkPhaseBaseline:
		logger.Warnf("Benchmark baseline mode: disabling cache and enabling analytics only")
		params.Cache.Enabled = false
		params.Cache.JustDependency = false
		params.Analytics.Enabled = true
	case common.BenchmarkPhaseWarmup:
		logger.Infof("(i) Benchmark warmup phase: cache performance might not be ideal")
	}
}

type benchmarkPhaseFile struct {
	Phase string `json:"phase"`
}

func writeBenchmarkPhaseFile(phase string, logger log.Logger) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		logger.Debugf("Failed to get home directory for benchmark phase file: %v", err)

		return
	}

	dir := filepath.Join(homeDir, ".local", "state", "xcelerate", "benchmark")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Debugf("Failed to create benchmark phase dir: %v", err)

		return
	}

	data, err := json.Marshal(benchmarkPhaseFile{Phase: phase})
	if err != nil {
		logger.Debugf("Failed to marshal benchmark phase file: %v", err)

		return
	}

	filePath := filepath.Join(dir, "benchmark-phase.json")
	if err := os.WriteFile(filePath, data, 0o644); err != nil { //nolint:mnd,gosec
		logger.Debugf("Failed to write benchmark phase file: %v", err)

		return
	}

	logger.Debugf("Benchmark phase written to %s", filePath)
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
