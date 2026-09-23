package spawn

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// ErrUnderTest guards a fork bomb: os.Executable() under `go test` is the test
// binary, so one spawn reruns the package, which spawns again. Stub it instead.
var ErrUnderTest = errors.New("refusing to re-exec the test binary; stub the spawn instead")

// Detached puts svc in its own process group so it outlives the caller and can
// be signalled as a group.
//
//nolint:noctx // intentionally detached: the service must outlive this command
func Detached(svc Service) (int, error) {
	if underTest() {
		return 0, ErrUnderTest
	}

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

	// Reaped, not awaited: an unreaped child stays a zombie for our lifetime.
	go func() { _ = cmd.Wait() }()

	return pid, nil
}

// underTest keys off the flags the generated test main registers, rather than
// importing testing from production code.
func underTest() bool {
	return flag.Lookup("test.v") != nil
}
