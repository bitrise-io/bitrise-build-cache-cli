package reactnative

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/google/uuid"

	ccacheipc "github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
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
}

// Runner wraps a command execution with invocation ID injection, pre-run hooks,
// and post-run analytics.
type Runner struct {
	execFn             ExecFunc
	ccacheInvocationID string
	socket             ccacheSocket
	postRun            postRunHook
}

// NewRunner creates a Runner with production pre-run and post-run hooks.
func NewRunner(params RunnerParams) *Runner {
	return &Runner{
		execFn:             params.ExecFn,
		ccacheInvocationID: params.CcacheInvocationID,
		socket:             newSocketFromConfig(),
		postRun:            &postRunDeps{},
	}
}

// Run injects a BITRISE_INVOCATION_ID into environ and delegates execution to ExecFn.
// If invocationID is empty, a random UUID is used.
func (r *Runner) Run(args []string, invocationID string, environ []string) error {
	if invocationID == "" {
		invocationID = uuid.New().String()
	}

	fmt.Fprintf(os.Stderr, "React Native invocation ID: %s\n", invocationID)

	// Strip leading "--" separator (cobra passes it through with DisableFlagParsing)
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}

	if len(args) == 0 {
		return fmt.Errorf("missing arguments")
	}

	name, cmdArgs := args[0], args[1:]

	if r.socket != nil {
		ensureHelper(r.socket, invocationID, r.ccacheInvocationID)
		zeroCcacheStats()
	}

	start := time.Now()
	execErr := r.execFn(append(environ, "BITRISE_INVOCATION_ID="+invocationID), name, cmdArgs...)
	duration := time.Since(start)

	if r.postRun != nil {
		runPostHook(r.postRun, invocationID, args, duration, execErr, r.ccacheInvocationID)
	}

	return execErr
}

// ---------------------------------------------------------------------------
// Private — ccache socket interface and helpers
// ---------------------------------------------------------------------------

type ccacheSocket interface {
	IsListening() bool
	Start() error
	AwaitReady() bool
	HealthCheck(ctx context.Context) error
	SetInvocationID(ctx context.Context, parentID, childID string) error
}

func newSocketFromConfig() ccacheSocket {
	config, err := ccacheconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	if err != nil {
		return nil
	}

	return ccacheipc.NewSocket(config.IPCEndpoint)
}

func ensureHelper(socket ccacheSocket, rnInvocationID, ccacheInvocationID string) {
	if socket == nil {
		return
	}

	if !socket.IsListening() {
		if err := socket.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to start ccache storage helper: %v\n", err)

			return
		}

		if !socket.AwaitReady() {
			fmt.Fprintf(os.Stderr, "Warning: ccache storage helper did not become ready\n")
		}
	}

	if err := socket.HealthCheck(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: ccache storage helper health check failed: %v\n", err)
	}

	if err := socket.SetInvocationID(context.Background(), rnInvocationID, ccacheInvocationID); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to send invocation ID to storage helper: %v\n", err)
	}
}

func zeroCcacheStats() {
	path, err := exec.LookPath("ccache")
	if err != nil {
		return
	}

	if err := exec.CommandContext(context.Background(), path, "-z").Run(); err != nil { //nolint:gosec
		fmt.Fprintf(os.Stderr, "Warning: failed to reset ccache stats: %v\n", err)
	}
}
