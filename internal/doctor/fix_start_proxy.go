package doctor

import (
	"fmt"
	"os/exec"
	"syscall"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// StartProxyFixer starts the xcelerate proxy the way a build does: as a
// detached child of this process, not as a supervised service. That keeps it
// in the caller's resource coalition, which is the whole reason the proxy is
// no longer a LaunchAgent — see docs/daemon-latency.md.
type StartProxyFixer struct {
	// Start is a test seam; nil uses the real spawn.
	Start func(exe string) error
}

func (f StartProxyFixer) Fix() (string, error) {
	exe, err := utils.DefaultOsProxy{}.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve the CLI path: %w", err)
	}

	start := f.Start
	if start == nil {
		start = spawnDetachedProxy
	}

	if err := start(exe); err != nil {
		return "", fmt.Errorf("start the xcelerate proxy: %w", err)
	}

	return "started the xcelerate proxy (it serves this login session; a build would have started it too)", nil
}

//nolint:noctx // intentionally detached: the proxy must outlive this command
func spawnDetachedProxy(exe string) error {
	cmd := exec.Command(exe, "xcelerate", "start-proxy")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn: %w", err)
	}

	// Not waited on deliberately: the proxy runs until the session ends.
	go func() { _ = cmd.Wait() }()

	return nil
}
