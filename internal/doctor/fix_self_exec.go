package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// defaultRunSelf re-execs the running CLI binary with the supplied args,
// forwarding stdio so progress / errors land in the caller's terminal.
// context.Background is appropriate because the spawned subcommand owns its
// own lifetime — the parent doctor run does not impose a timeout on it.
func defaultRunSelf(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate cli executable: %w", err)
	}

	cmd := exec.CommandContext(context.Background(), exe, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("re-exec %v: %w", args, err)
	}

	return nil
}

func (d *Doctor) runSelf(args ...string) error {
	runner := d.RunSelf
	if runner == nil {
		runner = defaultRunSelf
	}

	return runner(args...)
}

// daemonUpFix re-execs `bitrise-build-cache daemon up` to start the registered
// services. If daemon install hasn't run yet, the subcommand errors with a
// "run install first" hint, which surfaces verbatim as FixError.
func (d *Doctor) daemonUpFix() (string, error) {
	if err := d.runSelf("daemon", "up"); err != nil {
		return "", err
	}

	return "ran `bitrise-build-cache daemon up`", nil
}

// updateFix re-execs `bitrise-build-cache update`, which detects brew vs
// installer.sh install and runs the matching upgrade flow.
func (d *Doctor) updateFix() (string, error) {
	if err := d.runSelf("update"); err != nil {
		return "", err
	}

	return "ran `bitrise-build-cache update`", nil
}
