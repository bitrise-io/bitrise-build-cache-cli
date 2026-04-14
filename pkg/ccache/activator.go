package ccache

import (
	"context"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
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
}

// Activator activates Bitrise Build Cache for C++ via ccache.
// Exported fields are optional dependencies — when nil/zero, production
// defaults are used. Set them in tests to inject mocks.
type Activator struct {
	Params         ActivatorParams
	Logger         log.Logger
	OsProxy        utils.OsProxy
	CommandFunc    utils.CommandFunc
	EncoderFactory utils.EncoderFactory
}

// NewActivator creates an Activator with production defaults.
func NewActivator(params ActivatorParams) *Activator {
	return &Activator{Params: params}
}

// Activate creates the ccache config and exports the required environment
// variables via envman.
func (a *Activator) Activate(ctx context.Context) error {
	logger, osProxy, commandFunc, encoderFactory := a.resolvedDeps()
	logger.EnableDebugLog(a.Params.DebugLogging)
	logger.TInfof("Activate Bitrise Build Cache for C++")

	envs := a.Params.Envs
	if envs == nil {
		envs = utils.AllEnvs()
	}

	config, err := ccacheconfig.NewConfig(envs, osProxy, ccacheconfig.Params{
		BuildCacheEndpoint:    a.Params.BuildCacheEndpoint,
		PushEnabled:           a.Params.PushEnabled,
		IPCSocketPathOverride: a.Params.IPCSocketPathOverride,
		BaseDirOverride:       a.Params.BaseDirOverride,
	})
	if err != nil {
		return fmt.Errorf("failed to create ccache config: %w", err)
	}

	config.DebugLogging = a.Params.DebugLogging

	if err := config.Save(logger, osProxy, encoderFactory); err != nil {
		return fmt.Errorf("failed to save ccache config: %w", err)
	}

	baseDir := a.Params.BaseDirOverride
	if baseDir == "" {
		wd, err := osProxy.Getwd()
		if err != nil {
			logger.Warnf("Failed to get working directory for CCACHE_BASEDIR: %s", err)
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
		addEnvVarToEnvman(ctx, commandFunc, key, value, logger)
	}

	logger.TInfof(ActivateCppSuccessful)

	return nil
}

// ActivateCppSuccessful is the success message printed after activation.
const ActivateCppSuccessful = "✅ Bitrise Build Cache for C++ activated"

// ---------------------------------------------------------------------------
// Private — Activator methods
// ---------------------------------------------------------------------------

func (a *Activator) resolvedDeps() (log.Logger, utils.OsProxy, utils.CommandFunc, utils.EncoderFactory) {
	logger := a.Logger
	if logger == nil {
		logger = log.NewLogger(log.WithDebugLog(a.Params.DebugLogging))
	}

	osProxy := a.OsProxy
	if osProxy == nil {
		osProxy = utils.DefaultOsProxy{}
	}

	commandFunc := a.CommandFunc
	if commandFunc == nil {
		commandFunc = utils.DefaultCommandFunc()
	}

	encoderFactory := a.EncoderFactory
	if encoderFactory == nil {
		encoderFactory = utils.DefaultEncoderFactory{}
	}

	return logger, osProxy, commandFunc, encoderFactory
}

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
