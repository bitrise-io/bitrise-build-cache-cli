package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

type UpdateFixer struct {
	RunSelf func(args ...string) error
}

func (f UpdateFixer) Fix() (string, error) {
	runner := f.RunSelf
	if runner == nil {
		runner = defaultRunSelf
	}

	if err := runner("update"); err != nil {
		return "", fmt.Errorf("update: %w", err)
	}

	return "ran `bitrise-build-cache update`", nil
}

//nolint:contextcheck // Fixer.Fix is ctx-less by design; Background is correct here.
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
