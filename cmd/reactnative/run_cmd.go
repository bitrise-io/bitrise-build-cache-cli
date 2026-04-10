package reactnative

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	ccacheipc "github.com/bitrise-io/bitrise-build-cache-cli/internal/ccache"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/internal/config/ccache"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

// ExecFunc runs a command with the given environment, executable name, and arguments.
type ExecFunc func(environ []string, name string, args ...string) error

// RunWithInvocationIDFn is the testable core of the run command. It injects a BITRISE_INVOCATION_ID
// into environ and delegates execution to execFn. If invocationID is empty, a random UUID is used.
// If preRunFn is non-nil, it is called with the invocation ID immediately before execution (e.g. to
// ensure the storage helper is running and zero ccache stats). If postRunFn is non-nil, it is called
// after the command completes with the invocation ID, args, duration, and any exec error.
func RunWithInvocationIDFn(args []string, invocationID string, environ []string, execFn ExecFunc, preRunFn func(string), postRunFn PostRunFn) error {
	if invocationID == "" {
		invocationID = uuid.New().String()
	}
	fmt.Fprintf(os.Stderr, "React Native invocation ID: %s\n", invocationID)

	if len(args) == 0 {
		return fmt.Errorf("missing arguments")
	}

	name, cmdArgs := args[0], args[1:]

	if preRunFn != nil {
		preRunFn(invocationID)
	}

	start := time.Now()
	execErr := execFn(append(environ, "BITRISE_INVOCATION_ID="+invocationID), name, cmdArgs...)
	duration := time.Since(start)

	if postRunFn != nil {
		postRunFn(invocationID, args, duration, execErr)
	}

	return execErr
}

// EnsureCcacheHelperDeps holds the injectable dependencies for building an ensure-ccache-helper function.
type EnsureCcacheHelperDeps struct {
	// SocketPath returns the IPC socket path, or an error if ccache is not configured (silently skipped).
	SocketPath func() (string, error)
	// IsListening checks whether the socket has an active listener.
	IsListening func(string) bool
	// StartHelper launches the storage helper as a background process.
	StartHelper func() error
	// AwaitReady polls until the socket is listening or a timeout elapses.
	AwaitReady func(string) bool
	// HealthCheck, if non-nil, sends a health-check request to confirm the server's request loop is ready.
	HealthCheck func(ctx context.Context, socketPath string) error
	// SendInvocationID, if non-nil, notifies the server of the new invocation ID so it can reset
	// session stats and set up per-invocation logging.
	SendInvocationID func(ctx context.Context, socketPath, parentID, childID string) error
}

// Build returns a function that ensures the ccache storage helper is running, then sends a
// health check and SetInvocationID to the server so each run starts with a clean session.
// The returned function takes the react-native invocation ID and a pre-generated ccache
// invocation ID; both are passed to the storage helper.
func (d EnsureCcacheHelperDeps) Build() func(rnInvocationID, ccacheInvocationID string) {
	return func(rnInvocationID, ccacheInvocationID string) {
		socketPath, err := d.SocketPath()
		if err != nil {
			return // ccache not configured, skip silently
		}

		if !d.IsListening(socketPath) {
			if err := d.StartHelper(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to start ccache storage helper: %v\n", err)

				return
			}
			if !d.AwaitReady(socketPath) {
				fmt.Fprintf(os.Stderr, "Warning: ccache storage helper did not become ready\n")
			}
		}

		if d.HealthCheck != nil {
			if err := d.HealthCheck(context.Background(), socketPath); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: ccache storage helper health check failed: %v\n", err)
			}
		}

		if d.SendInvocationID != nil {
			if err := d.SendInvocationID(context.Background(), socketPath, rnInvocationID, ccacheInvocationID); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to send invocation ID to storage helper: %v\n", err)
			}
		}
	}
}

// startStorageHelper launches the storage helper as a detached background process.
func startStorageHelper() error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	cmd := exec.Command(bin, "ccache", "storage-helper", "start") //nolint:gosec,noctx // intentionally detached: the helper must outlive this command
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start storage helper process: %w", err)
	}

	return nil
}

// zeroCcacheStats resets ccache's internal counters so each run starts from a clean slate.
// If ccache is not on PATH, this is a no-op.
func zeroCcacheStats() {
	path, err := exec.LookPath("ccache")
	if err != nil {
		return // ccache not available, skip silently
	}

	if err := exec.CommandContext(context.Background(), path, "-z").Run(); err != nil { //nolint:gosec
		fmt.Fprintf(os.Stderr, "Warning: failed to reset ccache stats: %v\n", err)
	}
}

// awaitListening polls the socket until it is listening or a 5-second timeout elapses.
func awaitListening(socketPath string) bool {
	const timeout = 5 * time.Second
	const interval = 100 * time.Millisecond
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ccacheipc.IsListening(socketPath) {
			return true
		}
		time.Sleep(interval)
	}

	return false
}

//nolint:gochecknoglobals
var ensureCcacheHelper = EnsureCcacheHelperDeps{
	SocketPath: func() (string, error) {
		config, err := ccacheconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
		if err != nil {
			return "", fmt.Errorf("read ccache config: %w", err)
		}

		return config.IPCEndpoint, nil
	},
	IsListening:      ccacheipc.IsListening,
	StartHelper:      startStorageHelper,
	AwaitReady:       awaitListening,
	HealthCheck:      ccacheipc.SendHealthCheck,
	SendInvocationID: ccacheipc.SendInvocationID,
}.Build()

//nolint:gochecknoglobals
var runCmd = &cobra.Command{
	Use:                "run",
	Short:              "Run a process with the provided arguments",
	Long:               `Run a process, forwarding all provided arguments directly.`,
	SilenceUsage:       true,
	DisableFlagParsing: true,
	RunE: func(_ *cobra.Command, args []string) error {
		ccacheInvocationID := uuid.New().String()
		deps := defaultPostRunDeps
		deps.CcacheInvocationID = ccacheInvocationID

		return RunWithInvocationIDFn(args, os.Getenv("BITRISE_INVOCATION_ID"), os.Environ(), func(environ []string, name string, cmdArgs ...string) error {
			cmd := exec.Command(name, cmdArgs...) //nolint:gosec
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Env = environ
			if err := cmd.Run(); err != nil {
				var exitError *exec.ExitError
				if errors.As(err, &exitError) {
					os.Exit(exitError.ExitCode())
				}

				return fmt.Errorf("failed to run: %w", err)
			}

			return nil
		}, func(rnInvocationID string) {
			ensureCcacheHelper(rnInvocationID, ccacheInvocationID)
			zeroCcacheStats()
		}, deps.Build())
	},
}

func init() {
	reactNativeCmd.AddCommand(runCmd)
}
