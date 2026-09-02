package doctor

import (
	"fmt"
	"os/exec"
	"syscall"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// StartServiceFixer starts a cache service the way a build does: as a detached
// child of this process, not as a supervised one. That keeps it in the caller's
// resource coalition, which is the whole reason neither service is a LaunchAgent
// any more — see docs/daemon-latency.md.
type StartServiceFixer struct {
	Name string
	Args []string

	// Start is a test seam; nil uses the real spawn.
	Start func(exe string, args []string) error
}

// StartProxyFixer starts the xcelerate proxy.
func StartProxyFixer() StartServiceFixer {
	return StartServiceFixer{Name: "xcelerate proxy", Args: []string{"xcelerate", "start-proxy"}}
}

// StartCcacheHelperFixer starts the ccache storage helper.
func StartCcacheHelperFixer() StartServiceFixer {
	return StartServiceFixer{Name: "ccache storage helper", Args: []string{"ccache", "storage-helper", "start"}}
}

func (f StartServiceFixer) Fix() (string, error) {
	exe, err := utils.DefaultOsProxy{}.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve the CLI path: %w", err)
	}

	start := f.Start
	if start == nil {
		start = spawnDetached
	}

	if err := start(exe, f.Args); err != nil {
		return "", fmt.Errorf("start the %s: %w", f.Name, err)
	}

	return fmt.Sprintf("started the %s (it serves this login session; a build would have started it too)", f.Name), nil
}

//nolint:noctx // intentionally detached: the service must outlive this command
func spawnDetached(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn: %w", err)
	}

	// Not waited on deliberately: the service runs until the session ends.
	go func() { _ = cmd.Wait() }()

	return nil
}
