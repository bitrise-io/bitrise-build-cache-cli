package oauth

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// OpenBrowser opens url in the system default browser. It uses CommandContext
// with a background context (the launched browser must outlive any request
// context), satisfying the noctx linter without killing the browser on cancel.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(context.Background(), "open", url) //nolint:gosec // G204: fixed command, URL from our own authorize endpoint
	case "linux":
		cmd = exec.CommandContext(context.Background(), "xdg-open", url) //nolint:gosec // G204: fixed command, URL from our own authorize endpoint
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}

	return nil
}
