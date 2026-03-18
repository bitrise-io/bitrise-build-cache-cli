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
// into environ and delegates execution to execFn. If notifyFn is non-nil, it is called
// with the generated invocation ID before the command runs. If preRunFn is non-nil, it is
// called immediately before execution (e.g. to zero ccache stats). If postRunFn is non-nil, it is
// called after the command completes with the invocation ID, args, duration, and any exec error.
func RunWithInvocationIDFn(args []string, environ []string, execFn ExecFunc, notifyFn func(string), preRunFn func(), postRunFn PostRunFn) error {
	invocationID := uuid.New().String()
	fmt.Fprintf(os.Stderr, "Invocation ID: %s\n", invocationID)

	if notifyFn != nil {
		notifyFn(invocationID)
	}

	if len(args) == 0 {
		return fmt.Errorf("missing arguments")
	}

	name, cmdArgs := args[0], args[1:]

	if preRunFn != nil {
		preRunFn()
	}

	start := time.Now()
	execErr := execFn(append(environ, "BITRISE_INVOCATION_ID="+invocationID), name, cmdArgs...)
	duration := time.Since(start)

	if postRunFn != nil {
		postRunFn(invocationID, args, duration, execErr)
	}

	return execErr
}

// BuildNotifyCcacheHelperFn constructs a notifyCcacheHelper function with injectable dependencies.
// socketPathFn returns the IPC socket path, or an error if ccache is not configured (silently skipped).
// isListeningFn checks whether the socket has an active listener.
// startHelperFn launches the storage helper as a background process.
// awaitReadyFn polls until the socket is listening or a timeout elapses.
// sendInvocationIDFn sends the parent→child invocation ID pair over the socket.
func BuildNotifyCcacheHelperFn(
	socketPathFn func() (string, error),
	isListeningFn func(string) bool,
	startHelperFn func() error,
	awaitReadyFn func(string) bool,
	sendInvocationIDFn func(socketPath, parentID, childID string) error,
) func(string) {
	return func(parentID string) {
		socketPath, err := socketPathFn()
		if err != nil {
			return // ccache not configured, skip silently
		}

		if !isListeningFn(socketPath) {
			if err := startHelperFn(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to start ccache storage helper: %v\n", err)

				return
			}
			if !awaitReadyFn(socketPath) {
				fmt.Fprintf(os.Stderr, "Warning: ccache storage helper did not become ready\n")

				return
			}
		}

		childID := uuid.New().String()
		if err := sendInvocationIDFn(socketPath, parentID, childID); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: ccache helper notification failed: %v\n", err)
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

// zeroCcacheStats resets ccache's internal counters so each invocation starts from a clean slate.
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

//nolint:gochecknoglobals
var notifyCcacheHelper = BuildNotifyCcacheHelperFn(
	func() (string, error) {
		config, err := ccacheconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
		if err != nil {
			return "", fmt.Errorf("read ccache config: %w", err)
		}

		return config.IPCEndpoint, nil
	},
	ccacheipc.IsListening,
	startStorageHelper,
	awaitListening,
	func(socketPath, parentID, childID string) error {
		return ccacheipc.SendInvocationID(context.Background(), socketPath, parentID, childID)
	},
)

//nolint:gochecknoglobals
var runCmd = &cobra.Command{
	Use:                "run",
	Short:              "Run a process with the provided arguments",
	Long:               `Run a process, forwarding all provided arguments directly.`,
	SilenceUsage:       true,
	DisableFlagParsing: true,
	RunE: func(_ *cobra.Command, args []string) error {
		return RunWithInvocationIDFn(args, os.Environ(), func(environ []string, name string, cmdArgs ...string) error {
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
		}, notifyCcacheHelper, zeroCcacheStats, defaultPostRunFn)
	},
}

func init() {
	reactNativeCmd.AddCommand(runCmd)
}
