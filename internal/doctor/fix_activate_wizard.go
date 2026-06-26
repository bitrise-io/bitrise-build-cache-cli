package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

func (d *Doctor) activateWizardFix() (string, error) {
	launcher := d.LaunchActivateWizard
	if launcher == nil {
		launcher = defaultLaunchActivateWizard
	}

	if err := launcher(); err != nil {
		return "", fmt.Errorf("launch activate --interactive: %w", err)
	}

	return "ran `bitrise-build-cache activate --interactive`", nil
}

//nolint:contextcheck // Check.Fix is ctx-less by design; Background is correct here.
func defaultLaunchActivateWizard() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate cli executable: %w", err)
	}

	cmd := exec.CommandContext(context.Background(), exe, "activate", "--interactive")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("activate --interactive: %w", err)
	}

	return nil
}
