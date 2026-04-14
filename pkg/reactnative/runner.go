package reactnative

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"

	ccacheipc "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/ccache"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// ExecFunc runs a command with the given environment, executable name, and arguments.
type ExecFunc func(environ []string, name string, args ...string) error

// RunnerParams holds the configuration for creating a Runner.
type RunnerParams struct {
	ExecFn             ExecFunc
	CcacheInvocationID string

	// Logger overrides the default logger. If nil, a default logger is created.
	Logger log.Logger
	// OsProxy overrides the default OS proxy. If nil, utils.DefaultOsProxy{} is used.
	OsProxy utils.OsProxy
	// DecoderFactory overrides the default decoder factory. If nil, utils.DefaultDecoderFactory{} is used.
	DecoderFactory utils.DecoderFactory
}

//go:generate moq -stub -out post_run_runner_mock_test.go -pkg reactnative . postRunRunner

type postRunRunner interface {
	run(invocationID string, args []string, duration time.Duration, execErr error, ccacheInvocationID string)
}

// Runner wraps a command execution with invocation ID injection, pre-run hooks,
// and post-run analytics.
type Runner struct {
	execFn             ExecFunc
	ccacheInvocationID string
	logger             log.Logger
	osProxy            utils.OsProxy
	decoderFactory     utils.DecoderFactory
	socket             ccacheSocket
	postRun            postRunRunner
}

// NewRunner creates a Runner with production pre-run and post-run hooks.
func NewRunner(params RunnerParams) *Runner {
	logger := params.Logger
	if logger == nil {
		logger = log.NewLogger()
	}

	osProxy := params.OsProxy
	if osProxy == nil {
		osProxy = utils.DefaultOsProxy{}
	}

	decoderFactory := params.DecoderFactory
	if decoderFactory == nil {
		decoderFactory = utils.DefaultDecoderFactory{}
	}

	r := &Runner{
		execFn:             params.ExecFn,
		ccacheInvocationID: params.CcacheInvocationID,
		logger:             logger,
		osProxy:            osProxy,
		decoderFactory:     decoderFactory,
		postRun:            newPostRunDeps(logger, osProxy, decoderFactory),
	}
	r.socket = r.newSocketFromConfig()

	return r
}

// Run injects a BITRISE_INVOCATION_ID into environ and delegates execution to ExecFn.
// If invocationID is empty, a random UUID is used.
func (r *Runner) Run(args []string, invocationID string, environ []string) error {
	if invocationID == "" {
		r.logger.TInfof("No invocation ID provided, generating a random one")

		invocationID = uuid.New().String()
	}

	r.logger.TInfof("React Native invocation ID: %s", invocationID)

	// Strip leading "--" separator (cobra passes it through with DisableFlagParsing)
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}

	if len(args) == 0 {
		return fmt.Errorf("missing arguments")
	}

	name, cmdArgs := args[0], args[1:]

	if r.socket != nil {
		r.ensureHelper(invocationID)
		r.zeroCcacheStats()
	}

	start := time.Now()
	execErr := r.execFn(append(environ, "BITRISE_INVOCATION_ID="+invocationID), name, cmdArgs...)
	duration := time.Since(start)

	if r.postRun != nil {
		r.postRun.run(invocationID, args, duration, execErr, r.ccacheInvocationID)
	}

	return execErr
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

func (r *Runner) newSocketFromConfig() ccacheSocket {
	config, err := ccacheconfig.ReadConfig(r.osProxy, r.decoderFactory)
	if err != nil {
		return nil
	}

	return ccacheipc.NewSocket(config.IPCEndpoint)
}

func (r *Runner) ensureHelper(rnInvocationID string) {
	if !r.socket.IsListening() {
		if err := r.socket.Start(); err != nil {
			r.logger.TWarnf("Failed to start ccache storage helper: %v", err)

			return
		}

		if !r.socket.AwaitReady() {
			r.logger.TWarnf("Ccache storage helper did not become ready")
		}
	}

	if err := r.socket.HealthCheck(context.Background()); err != nil {
		r.logger.TWarnf("Ccache storage helper health check failed: %v", err)
	}

	if err := r.socket.SetInvocationID(context.Background(), rnInvocationID, r.ccacheInvocationID); err != nil {
		r.logger.TWarnf("Failed to send invocation ID to storage helper: %v", err)
	}
}

func (r *Runner) zeroCcacheStats() {
	path, err := exec.LookPath("ccache")
	if err != nil {
		return
	}

	if err := exec.CommandContext(context.Background(), path, "-z").Run(); err != nil { //nolint:gosec
		r.logger.TWarnf("Failed to reset ccache stats: %v", err)
	}
}
