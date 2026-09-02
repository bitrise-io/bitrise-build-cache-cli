package spawn

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// Detached starts svc in its own process group so it outlives the caller and
// can be signalled as a group.
//
//nolint:noctx // intentionally detached: the service must outlive this command
func Detached(svc Service) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("resolve the CLI path: %w", err)
	}

	cmd := exec.Command(exe, svc.Args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start %s: %w", svc.Name, err)
	}

	pid := cmd.Process.Pid

	// Not waited on deliberately, but reaped so the service does not linger as
	// a zombie for the lifetime of this process.
	go func() { _ = cmd.Wait() }()

	return pid, nil
}
