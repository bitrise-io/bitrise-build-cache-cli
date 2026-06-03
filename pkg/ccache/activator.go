package ccache

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/ccache"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// ActivatorParams configures the C++ build cache activation.
type ActivatorParams struct {
	BuildCacheEndpoint    string
	PushEnabled           bool
	IPCSocketPathOverride string
	BaseDirOverride       string
	DebugLogging          bool
	Envs                  map[string]string

	// Logger overrides the default logger. If nil, a default logger is created.
	Logger log.Logger
	// OsProxy overrides the default OS proxy. If nil, utils.DefaultOsProxy{} is used.
	OsProxy utils.OsProxy
	// CommandFunc overrides the default command function. If nil, utils.DefaultCommandFunc() is used.
	CommandFunc utils.CommandFunc
	// EncoderFactory overrides the default encoder factory. If nil, utils.DefaultEncoderFactory{} is used.
	EncoderFactory utils.EncoderFactory
}

// Activator activates Bitrise Build Cache for C++ via ccache.
type Activator struct {
	logger         log.Logger
	osProxy        utils.OsProxy
	commandFunc    utils.CommandFunc
	encoderFactory utils.EncoderFactory

	buildCacheEndpoint    string
	pushEnabled           bool
	ipcSocketPathOverride string
	baseDirOverride       string
	debugLogging          bool
	envs                  map[string]string
}

// NewActivator creates an Activator with production defaults.
func NewActivator(params ActivatorParams) *Activator {
	envs := params.Envs
	if envs == nil {
		envs = utils.AllEnvs()
	}

	logger := params.Logger
	if logger == nil {
		logger = log.NewLogger(log.WithDebugLog(params.DebugLogging))
	}

	osProxy := params.OsProxy
	if osProxy == nil {
		osProxy = utils.DefaultOsProxy{}
	}

	commandFunc := params.CommandFunc
	if commandFunc == nil {
		commandFunc = utils.DefaultCommandFunc()
	}

	encoderFactory := params.EncoderFactory
	if encoderFactory == nil {
		encoderFactory = utils.DefaultEncoderFactory{}
	}

	return &Activator{
		logger:         logger,
		osProxy:        osProxy,
		commandFunc:    commandFunc,
		encoderFactory: encoderFactory,

		buildCacheEndpoint:    params.BuildCacheEndpoint,
		pushEnabled:           params.PushEnabled,
		ipcSocketPathOverride: params.IPCSocketPathOverride,
		baseDirOverride:       params.BaseDirOverride,
		debugLogging:          params.DebugLogging,
		envs:                  envs,
	}
}

// Activate creates the ccache config and exports the required environment
// variables via envman.
func (a *Activator) Activate(ctx context.Context) error {
	configcommon.LogCLIVersion(a.logger)
	a.logger.TInfof("Activate Bitrise Build Cache for C++")

	config, err := ccacheconfig.NewConfig(a.envs, a.osProxy, ccacheconfig.Params{
		BuildCacheEndpoint:    a.buildCacheEndpoint,
		PushEnabled:           a.pushEnabled,
		IPCSocketPathOverride: a.ipcSocketPathOverride,
		BaseDirOverride:       a.baseDirOverride,
	})
	if err != nil {
		return fmt.Errorf("failed to create ccache config: %w", err)
	}

	config.DebugLogging = a.debugLogging

	if err := config.Save(a.logger, a.osProxy, a.encoderFactory); err != nil {
		return fmt.Errorf("failed to save ccache config: %w", err)
	}

	// Auth credentials are persisted only in the multiplatform analytics config
	// (single source of truth on disk). The ccache config carries auth in-memory
	// at runtime via ReadConfig, but never to JSON.
	mpCfg := multiplatformconfig.Config{
		AuthConfig:   config.AuthConfig,
		DebugLogging: a.debugLogging,
	}
	if err := mpCfg.Save(a.osProxy, a.encoderFactory); err != nil {
		return fmt.Errorf("failed to save multiplatform analytics config: %w", err)
	}

	baseDir := a.baseDirOverride
	if baseDir == "" {
		wd, err := a.osProxy.Getwd()
		if err != nil {
			a.logger.Warnf("Failed to get working directory for CCACHE_BASEDIR: %s", err)
		} else {
			baseDir = wd
		}
	}

	envVars := map[string]string{
		"CCACHE_BASEDIR":              baseDir,
		"CCACHE_NOHASHDIR":            "true",
		"CCACHE_REMOTE_ONLY":          "true",
		"CCACHE_REMOTE_STORAGE":       config.CRSHRemoteStorageURL(),
		"CMAKE_CXX_COMPILER_LAUNCHER": "ccache",
		"CMAKE_C_COMPILER_LAUNCHER":   "ccache",
	}

	for key, value := range envVars {
		addEnvVarToEnvman(ctx, a.commandFunc, key, value, a.logger)
	}

	a.logger.TInfof(ActivateCppSuccessful)

	return nil
}

// ActivateCppSuccessful is the success message printed after activation.
const ActivateCppSuccessful = "✅ Bitrise Build Cache for C++ activated"

// ---------------------------------------------------------------------------
// Private — package-level helpers
// ---------------------------------------------------------------------------

func addEnvVarToEnvman(
	ctx context.Context,
	commandFunc utils.CommandFunc,
	key, value string,
	logger log.Logger,
) {
	command := commandFunc(ctx, "envman", "add", "--key", key, "--value", value)
	if output, err := command.CombinedOutput(); err != nil {
		logger.Debugf("Failed to run envman add for %s: %s", key, string(output))

		return
	}

	logger.TInfof("Set %s=%s via envman", key, value)
}
