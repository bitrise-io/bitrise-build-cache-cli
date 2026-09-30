package gradleconfig

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	authpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/clibin"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/envexport"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const (
	errFmtInvalidCacheLevel        = "invalid cache validation level, valid options: none, warning, error"
	errFmtTestDistroPoolName       = "test distribution plugin was enabled but pool name must be non-blank (use --test-distribution-pool)"
	ErrFmtReadAuthConfig           = "resolve auth config: %w"
	errFmtCacheConfigCreation      = "couldn't create cache configuration: %w"
	errFmtTestDistroConfigCreation = "couldn't create test distribution configuration: %w"
	errFmtInvalidValidationLevel   = "invalid validation level: '%s'"
)

var errLiteNeedsReachableCLI = errors.New(
	"lite activation bakes no auth token, so the Gradle plugins resolve one by running the CLI at " +
		"configuration time; install it on $PATH (e.g. /usr/local/bin) before running activate --lite")

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
	PoolName        string
}

type ActivateGradleParams struct {
	Cache      CacheParams
	Analytics  AnalyticsParams
	TestDistro TestDistroParams

	CLIPath string

	// Lite writes the static wiring only: no credential resolution or pinning, no
	// benchmark query, no envman export. Everything workspace-, build- or
	// credential-specific is left to the plugins to resolve mid-build.
	Lite bool
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

	authConfig, authOrigin, _, err := resolver.ResolveAllowingNone(ctx, envs, params.Lite)
	if err != nil {
		return TemplateInventory{}, fmt.Errorf(ErrFmtReadAuthConfig, err)
	}

	// ResolveUsername can reach the OS keychain, which has been seen to hang on a
	// macOS CI agent. Preboot emits no build user anyway, so do not ask.
	var username string
	if !params.Lite {
		username, _ = resolver.ResolveUsername(envs)
	}

	metadata := common.NewMetadata(envs, username,
		func(name string, v ...string) (string, error) {
			output, err := exec.Command(name, v...).Output() //nolint:noctx

			return string(output), err
		},
		osProxy,
		logger)
	logger.Infof("(i) Cache Config: %+v", metadata)

	// Check benchmark phase and override params if needed (only on CI)
	if !params.Lite && metadata.CIProvider != "" && benchmarkProvider != nil {
		logger.Debugf("Checking benchmark phase...CI Provider: %s", metadata.CIProvider)
		ApplyBenchmarkPhase(&params, logger, benchmarkProvider, metadata, envexport.New(envs, logger))
	}

	// Never opt-in under lite, whatever the machine says. Opt-in renders a
	// scope-check ValueSource that shells out to the CLI on every Gradle
	// configuration, and it answers a question — "did this developer mark this
	// checkout?" — that has no meaning on a VM about to be handed an arbitrary
	// build. The flag is refused outright at the command layer; this covers a
	// mode some earlier activation left on the machine.
	projectMode := machineconfig.ModeAlways
	if !params.Lite {
		projectMode = resolveProjectMode(osProxy, logger)
	}

	commonInventory, err := params.commonTemplateInventory(authConfig, authOrigin, metadata, isDebug, projectMode)
	if err != nil {
		return TemplateInventory{}, err
	}

	cacheInventory, err := params.cacheTemplateInventory(logger, envs)
	if err != nil {
		return TemplateInventory{}, fmt.Errorf(errFmtCacheConfigCreation, err)
	}

	analyticsInventory := params.analyticsTemplateInventory(logger)

	testDistroInventory, err := params.testDistroTemplateInventory(logger, isDebug)
	if err != nil {
		return TemplateInventory{}, fmt.Errorf(errFmtTestDistroConfigCreation, err)
	}

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
) (PluginCommonTemplateInventory, error) {
	cliPath := params.CLIPath
	if cliPath == "" {
		cliPath = paths.CLIBinaryName
	}

	// Lite bakes no token, so every plugin that needs one shells out to the CLI
	// at configuration time. A bare name that resolves to nothing there turns
	// into a per-build auth failure inside Gradle; say so now instead.
	if params.Lite && params.CLIPath == "" && !clibin.OnPATH() && params.needsAuthToken() {
		return PluginCommonTemplateInventory{}, errLiteNeedsReachableCLI
	}

	// Structural, not environmental: preboot must emit no credential and no build
	// identity even when it happens to run inside a build that has both. An empty
	// CIProvider is also what lets the plugins run their own CI detection.
	if params.Lite {
		authConfig, authOrigin = authpkg.Credential{}, authpkg.Origin{}
		metadata = common.CacheConfigMetadata{}
	}

	return PluginCommonTemplateInventory{
		AuthToken:   authpkg.GradleToken(authConfig, authOrigin),
		Debug:       isDebug,
		AppSlug:     metadata.BitriseAppID,
		CIProvider:  metadata.CIProvider,
		Version:     consts.GradleCommonPluginDepVersion,
		CLIPath:     cliPath,
		ProjectMode: string(projectMode),
		Lite:        params.Lite,
	}, nil
}

// needsAuthToken reports whether any enabled plugin has to authenticate.
func (params ActivateGradleParams) needsAuthToken() bool {
	return params.Cache.Enabled || params.Analytics.Enabled || params.TestDistro.Enabled
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

	if strings.TrimSpace(params.TestDistro.PoolName) == "" {
		return TestDistroTemplateInventory{}, errors.New(errFmtTestDistroPoolName)
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
		PoolName:        strings.TrimSpace(params.TestDistro.PoolName),
	}, nil
}
