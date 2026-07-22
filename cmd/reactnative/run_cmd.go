package reactnative

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	rnpkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/reactnative"
)

//nolint:gochecknoglobals
var runCmd = &cobra.Command{
	Use:                "run",
	Short:              "Run a process with the provided arguments",
	Long:               `Run a process, forwarding all provided arguments directly.`,
	SilenceUsage:       true,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		r := rnpkg.NewRunner(rnpkg.RunnerParams{
			ExecFn: defaultExecFn,
		})

		exitCode, err := r.Run(cmd.Context(), args, os.Getenv("BITRISE_INVOCATION_ID"), os.Environ())

		// The post-run hook (local invocation log + BE analytics) has already
		// fired inside Runner.Run by this point, so it's safe to terminate
		// the process. A non-zero child exit is not a wrapper error — the
		// child already wrote its own output — so we skip cobra's error path
		// and exit directly with the child's status.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitCode)
		}

		if err != nil {
			return err //nolint:wrapcheck // Runner.Run already returns wrapped errors
		}

		if exitCode != 0 {
			os.Exit(exitCode)
		}

		return nil
	},
}

func init() {
	reactNativeCmd.AddCommand(runCmd)
}

func defaultExecFn(environ []string, name string, cmdArgs ...string) (int, error) {
	cmd := exec.Command(name, cmdArgs...) //nolint:gosec,noctx // exec context intentionally not used: the child process should not be cancelled by the CLI's context
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = environ

	if err := cmd.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return exitError.ExitCode(), err //nolint:wrapcheck // ExitError intentionally propagated so post-run hook sees failure
		}

		return 1, fmt.Errorf("failed to run: %w", err)
	}

	return 0, nil
}
