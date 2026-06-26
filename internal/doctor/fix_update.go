package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

func (d *Doctor) updateFix() (string, error) {
	if err := d.runSelf("update"); err != nil {
		return "", err
	}

	return "ran `bitrise-build-cache update`", nil
}

func (d *Doctor) runSelf(args ...string) error {
	runner := d.RunSelf
	if runner == nil {
		runner = defaultRunSelf
	}

	return runner(args...)
}

//nolint:contextcheck // Check.Fix is ctx-less by design; Background is correct here.
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
