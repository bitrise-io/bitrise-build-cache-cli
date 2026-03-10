package reactnative

import (
	"fmt"
	"os"
	"os/exec"

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

// notifyCcacheHelper sends the invocation ID to the ccache storage helper socket.
// Silently skips if ccache is not configured. Logs a warning if the send fails.
func notifyCcacheHelper(invocationID string) {
	config, err := ccacheconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	if err != nil {
		return // ccache not configured, skip silently
	}
	if err := ccacheipc.SendInvocationID(config.IPCEndpoint, invocationID); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: ccache helper notification failed: %v\n", err)
	}
}

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
