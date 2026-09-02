package ccache

import (
	"context"
	"fmt"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/live"
	ccacheipc "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/ccache"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/ccache"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/spawn"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
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

	a.ensureLogDir()

	// Materialise an env- or JWT-sourced credential: the detached storage helper
	// starts in a shell that never saw those variables.
	if _, _, err := live.Default(a.logger).ResolvePinned(ctx, a.envs, configcommon.DetectCIProvider(a.envs) != ""); err != nil {
		return fmt.Errorf("persist auth credentials: %w", err)
	}

	// Read-modify-write: Config.Save is a full overwrite, and a fresh Config here
	// would drop the credentials block that is the only credential store on a
	// keychain-less host.
	if err := multiplatformconfig.Update(a.osProxy, a.encoderFactory, utils.DefaultDecoderFactory{}, func(c *multiplatformconfig.Config) {
		c.DebugLogging = a.debugLogging
	}); err != nil {
		return fmt.Errorf("failed to save multiplatform analytics config: %w", err)
	}
	a.logger.Infof("Wrote multiplatform analytics config: %s", multiplatformconfig.FilePath(a.osProxy))

	baseDir := a.baseDirOverride
	if baseDir == "" {
		wd, err := a.osProxy.Getwd()
		if err != nil {
			a.logger.Warnf("Failed to get working directory for CCACHE_BASEDIR: %s", err)
		} else {
			baseDir = wd
		}
	}

	for key, value := range config.BuildEnv(baseDir) {
		addEnvVarToEnvman(ctx, a.commandFunc, key, value, a.logger)
	}

	a.ensureHelperServing(ctx, config.IPCEndpoint)

	a.logger.TInfof(ActivateCppSuccessful)

	return nil
}

const (
	helperReadyBudget   = 5 * time.Second
	helperReadyInterval = 100 * time.Millisecond
)

// ensureHelperServing starts the storage helper if nothing answers its socket.
// ccache silently misses every lookup when the socket is dead, so this is
// best-effort but must not fail activation.
func (a *Activator) ensureHelperServing(ctx context.Context, socketPath string) {
	if p, err := paths.Default(); err == nil {
		if spawn.RemoveLegacySupervision(ctx, p, spawn.CcacheHelper()) {
			a.logger.Warnf("Removed a leftover service registration for the ccache storage helper.")
		}
	}

	if spawn.ProbeWith(ctx, socketPath, ccacheipc.SendHealthCheck) == spawn.Running {
		return
	}

	opts := []ccacheipc.StartOption{}
	if a.debugLogging {
		opts = append(opts, ccacheipc.WithDebug())
	}
	if invID := a.envs["BITRISE_INVOCATION_ID"]; invID != "" {
		opts = append(opts, ccacheipc.WithInvocationID(invID))
	}

	if err := ccacheipc.NewSocket(socketPath).Start(opts...); err != nil {
		a.logger.Warnf("Could not start the ccache storage helper: %s", err)

		return
	}

	if !spawn.AwaitSocketWith(ctx, socketPath, ccacheipc.SendHealthCheck, helperReadyBudget, helperReadyInterval) {
		a.logger.Warnf("The ccache storage helper did not become ready on %s", socketPath)

		return
	}

	a.logger.Debugf("Started the ccache storage helper on %s", socketPath)
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

// ensureLogDir creates the dir the storage helper would otherwise create on its
// first run, so the first build's health check doesn't report it as missing.
func (a *Activator) ensureLogDir() {
	home, err := a.osProxy.UserHomeDir()
	if err != nil {
		a.logger.Debugf("Could not resolve the home dir for the ccache log dir: %s", err)

		return
	}

	if err := paths.EnsureDir(a.osProxy, paths.FromHome(home).CcacheLogDir()); err != nil {
		a.logger.Debugf("Could not create the ccache log dir: %s", err)
	}
}
