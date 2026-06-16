package reactnative

import (
	"context"
	"fmt"
	osexec "os/exec"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"

	ccacheipc "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/ccache"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/ccache"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/multiplatform"
	rnconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/reactnative"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/exec"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

// MsgRNNotActivated is the warning emitted when `react-native run` is invoked
// before (or without) `activate react-native`. The user's command is still
// executed unchanged so the build doesn't fail; only the build-cache wrapping
// (ccache plumbing, invocation ID injection, analytics) is skipped.
const MsgRNNotActivated = "Bitrise Build Cache for React Native is not activated on this machine — running the wrapped command without build-cache instrumentation. Run `bitrise-build-cache activate react-native` (with a workspace ID configured) first to enable."

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// ExecFunc runs a command with the given environment, executable name, and arguments.
type ExecFunc func(environ []string, name string, args ...string) error

// RunnerParams holds the configuration for creating a Runner.
type RunnerParams struct {
	ExecFn ExecFunc

	// Logger overrides the default logger. If nil, a default logger is created.
	Logger log.Logger
	// OsProxy overrides the default OS proxy. If nil, utils.DefaultOsProxy{} is used.
	OsProxy utils.OsProxy
	// DecoderFactory overrides the default decoder factory. If nil, utils.DefaultDecoderFactory{} is used.
	DecoderFactory utils.DecoderFactory
}

//go:generate moq -stub -out post_run_runner_mock_test.go -pkg reactnative . postRunRunner

type postRunRunner interface {
	run(
		context context.Context,
		wrapperInvocationID string,
		args []string,
		duration time.Duration,
		execErr error,
	)
}

// Runner wraps a command execution with invocation ID injection, pre-run hooks,
// and post-run analytics.
type Runner struct {
	execFn         ExecFunc
	logger         log.Logger
	osProxy        utils.OsProxy
	decoderFactory utils.DecoderFactory
	postRun        postRunRunner
	ccacheConfig   *ccacheconfig.Config
	socket         ccacheSocket
}

// NewRunner creates a Runner with production pre-run and post-run hooks.
func NewRunner(params RunnerParams) *Runner {
	osProxy := params.OsProxy
	if osProxy == nil {
		osProxy = utils.DefaultOsProxy{}
	}

	decoderFactory := params.DecoderFactory
	if decoderFactory == nil {
		decoderFactory = utils.DefaultDecoderFactory{}
	}

	logger := params.Logger
	if logger == nil {
		// Honour DebugLogging from the multiplatform analytics config so
		// `activate react-native --debug` propagates to the runner and to any
		// component (e.g. the analytics client) that uses this logger.
		debug := false
		if cfg, err := multiplatformconfig.ReadConfig(osProxy, decoderFactory); err == nil {
			debug = cfg.DebugLogging
		}

		logger = log.NewLogger(log.WithDebugLog(debug))
	}

	var ccacheConfig *ccacheconfig.Config
	var socket ccacheSocket
	if config, err := ccacheconfig.ReadConfig(osProxy, decoderFactory); err == nil {
		ccacheConfig = &config
		socket = ccacheipc.NewSocket(config.IPCEndpoint)
	}

	r := &Runner{
		execFn:         params.ExecFn,
		logger:         logger,
		osProxy:        osProxy,
		decoderFactory: decoderFactory,
		ccacheConfig:   ccacheConfig,
		postRun:        newPostRunDeps(logger, osProxy, decoderFactory),
		socket:         socket,
	}

	return r
}

// Run injects a BITRISE_INVOCATION_ID into environ and delegates execution to ExecFn.
// If wrapperInvocationID is empty, a random UUID is used.
//
// When `activate react-native` was never run on this machine (or the
// activation didn't carry a workspace ID through), the wrapped command is
// still executed but every build-cache-related side-effect — ccache helper
// plumbing, invocation ID injection, analytics — is skipped. The user's
// build never fails just because the cache wasn't activated.
func (r *Runner) Run(ctx context.Context, args []string, wrapperInvocationID string, environ []string) error {
	configcommon.LogCLIVersion(r.logger)

	// Strip leading "--" separator (cobra passes it through with DisableFlagParsing)
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}

	if len(args) == 0 {
		return fmt.Errorf("missing arguments")
	}

	name, cmdArgs := args[0], args[1:]

	if !r.isReactNativeReady() {
		r.logger.TWarnf(MsgRNNotActivated)

		return r.execFn(environ, name, cmdArgs...)
	}

	if wrapperInvocationID == "" {
		r.logger.TInfof("No invocation ID provided, generating a random one")

		wrapperInvocationID = uuid.New().String()
	}

	r.logger.TInfof("React Native invocation ID: %s", wrapperInvocationID)

	if r.socket != nil {
		r.ensureHelper(ctx, wrapperInvocationID)
		r.zeroCcacheStats(ctx)
	}

	envMap := environToMap(environ)
	envMap["BITRISE_INVOCATION_ID"] = wrapperInvocationID
	r.maybeInjectEASWorkingDir(envMap, name, cmdArgs)

	start := time.Now()
	execErr := r.execFn(mapToEnviron(envMap), name, cmdArgs...)
	duration := time.Since(start)

	if r.postRun != nil {
		r.postRun.run(context.Background(), wrapperInvocationID, args, duration, execErr) //nolint:contextcheck // intentionally detached: post-run analytics must complete even if parent ctx is cancelled
	}

	return execErr
}

// isReactNativeReady reports whether `activate react-native` was run on this
// machine AND the multiplatform analytics config has the workspace slug we
// need to identify the user's workspace at analytics + UI URL time.
//
// Both signals must agree. The RN marker on its own only says "activate
// happened"; the multiplatform config carries the workspace slug that the
// post-run hook and the Visit-URL log line depend on. A missing or
// empty-workspace config means we cannot safely report analytics or print
// a working details URL, so we skip wrapping entirely (and log once).
func (r *Runner) isReactNativeReady() bool {
	cfg, err := rnconfig.ReadConfig(r.osProxy, r.decoderFactory)
	if err != nil || !cfg.Enabled {
		return false
	}

	mpCfg, err := multiplatformconfig.ReadConfig(r.osProxy, r.decoderFactory)
	if err != nil {
		return false
	}

	return mpCfg.AuthConfig.WorkspaceID != ""
}

// ---------------------------------------------------------------------------
// Private — Runner methods
// ---------------------------------------------------------------------------

type ccacheSocket interface {
	IsListening() bool
	Start() error
	AwaitReady() bool
	HealthCheck(ctx context.Context) error
	SetInvocationID(ctx context.Context, parentID, childID string) error
}

func (r *Runner) ensureHelper(ctx context.Context, wrapperInvocationID string) {
	socket := r.socket
	if socket == nil {
		return
	}

	if !socket.IsListening() {
		if err := socket.Start(); err != nil {
			r.logger.TWarnf("Failed to start ccache storage helper: %v", err)

			return
		}

		if !socket.AwaitReady() {
			r.logger.TWarnf("Ccache storage helper did not become ready")
		}
	}

	if err := socket.HealthCheck(ctx); err != nil {
		r.logger.TWarnf("Ccache storage helper health check failed: %v", err)
	}

	if err := socket.SetInvocationID(ctx, wrapperInvocationID, uuid.NewString()); err != nil {
		r.logger.TWarnf("Failed to send invocation ID to storage helper: %v", err)
	}
}

// maybeInjectEASWorkingDir pins EAS Build's local working directory to a
// stable path when the wrapped command is `eas build` (directly or via a
// package-manager runner). Without this, EAS picks a new tmp dir per
// invocation and every downstream cache (Gradle, Xcode, ccache) misses
// because absolute source paths feed into the cache keys.
//
// The injection is a no-op when the user has already set
// EAS_LOCAL_BUILD_WORKINGDIR — explicit user intent wins. It is also a no-op
// when HOME is missing from the environment (DefaultEASWorkingDir returns ""),
// because we have no safe path to pin to.
func (r *Runner) maybeInjectEASWorkingDir(envs map[string]string, name string, cmdArgs []string) {
	if !IsEASBuildInvocation(name, cmdArgs) {
		return
	}

	if _, alreadySet := envs[EASWorkingDirEnv]; alreadySet {
		return
	}

	workdir := DefaultEASWorkingDir(envs)
	if workdir == "" {
		return
	}

	r.logger.TInfof("Pinning %s=%s for EAS Build cache stability", EASWorkingDirEnv, workdir)
	envs[EASWorkingDirEnv] = workdir
}

func (r *Runner) zeroCcacheStats(ctx context.Context) {
	path, err := osexec.LookPath("ccache")
	if err != nil {
		return
	}

	if _, _, _, err := (exec.ExecRunner{}).Run(ctx, path, "-z"); err != nil {
		r.logger.TWarnf("Failed to reset ccache stats: %v", err)
	}
}
