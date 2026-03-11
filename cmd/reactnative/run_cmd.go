package reactnative

import (
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

// RunCmdFn is the testable core of the run command. It injects a BITRISE_INVOCATION_ID
// into environ and delegates execution to execFn. If notifyFn is non-nil, it is called
// with the generated invocation ID before the command runs.
func RunCmdFn(args []string, environ []string, execFn ExecFunc, notifyFn func(string)) error {
	invocationID := uuid.New().String()
	fmt.Fprintf(os.Stderr, "Invocation ID: %s\n", invocationID)

	if notifyFn != nil {
		notifyFn(invocationID)
	}

	if len(args) == 0 {
		return fmt.Errorf("missing arguments")
	}
	name, cmdArgs := args[0], args[1:]
	return execFn(append(environ, "BITRISE_INVOCATION_ID="+invocationID), name, cmdArgs...)
}

// BuildNotifyCcacheHelperFn constructs a notifyCcacheHelper function with injectable dependencies.
// socketPathFn returns the IPC socket path, or an error if ccache is not configured (silently skipped).
// isListeningFn checks whether the socket has an active listener.
// startHelperFn launches the storage helper as a background process.
// awaitReadyFn polls until the socket is listening or a timeout elapses.
// sendInvocationIDFn sends the invocation ID over the socket.
func BuildNotifyCcacheHelperFn(
	socketPathFn func() (string, error),
	isListeningFn func(string) bool,
	startHelperFn func() error,
	awaitReadyFn func(string) bool,
	sendInvocationIDFn func(string, string) error,
) func(string) {
	return func(invocationID string) {
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

		if err := sendInvocationIDFn(socketPath, invocationID); err != nil {
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
	cmd := exec.Command(bin, "ccache", "storage-helper", "start") //nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
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
var notifyCcacheHelper = BuildNotifyCcacheHelperFn(
	func() (string, error) {
		config, err := ccacheconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
		if err != nil {
			return "", err
		}
		return config.IPCEndpoint, nil
	},
	ccacheipc.IsListening,
	startStorageHelper,
	awaitListening,
	ccacheipc.SendInvocationID,
)

//nolint:gochecknoglobals
var runCmd = &cobra.Command{
	Use:                "run",
	Short:              "Run a process with the provided arguments",
	Long:               `Run a process, forwarding all provided arguments directly.`,
	SilenceUsage:       true,
	DisableFlagParsing: true,
	RunE: func(_ *cobra.Command, args []string) error {
		return RunCmdFn(args, os.Environ(), func(environ []string, name string, cmdArgs ...string) error {
			cmd := exec.Command(name, cmdArgs...) //nolint:gosec
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Env = environ
			if err := cmd.Run(); err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					os.Exit(exitErr.ExitCode())
				}
				return fmt.Errorf("failed to run: %w", err)
			}
			return nil
		}, notifyCcacheHelper)
	},
}

func init() {
	reactNativeCmd.AddCommand(runCmd)
}
