package reactnative

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	rnpkg "github.com/bitrise-io/bitrise-build-cache-cli/v2/pkg/reactnative"
)

//nolint:gochecknoglobals
var runCmd = &cobra.Command{
	Use:                "run",
	Short:              "Run a process with the provided arguments",
	Long:               `Run a process, forwarding all provided arguments directly.`,
	SilenceUsage:       true,
	DisableFlagParsing: true,
	RunE: func(_ *cobra.Command, args []string) error {
		r := rnpkg.NewRunner(rnpkg.RunnerParams{
			ExecFn:             defaultExecFn,
			CcacheInvocationID: uuid.New().String(),
		})

		return r.Run(args, os.Getenv("BITRISE_INVOCATION_ID"), os.Environ())
	},
}

func init() {
	reactNativeCmd.AddCommand(runCmd)
}

func defaultExecFn(environ []string, name string, cmdArgs ...string) error {
	cmd := exec.Command(name, cmdArgs...) //nolint:gosec,noctx // exec context intentionally not used: the child process should not be cancelled by the CLI's context
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
}
