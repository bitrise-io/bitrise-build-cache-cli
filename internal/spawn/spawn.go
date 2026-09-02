package spawn

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// ErrUnderTest is returned instead of re-execing a test binary. Detached
// re-execs os.Executable(), which under `go test` is the test binary itself: it
// would rerun the whole package, which would spawn again, exponentially. Tests
// must stub the spawn rather than reach this.
var ErrUnderTest = errors.New("refusing to re-exec the test binary; stub the spawn instead")

// Detached starts svc in its own process group so it outlives the caller and
// can be signalled as a group.
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

	// Not waited on deliberately, but reaped so the service does not linger as
	// a zombie for the lifetime of this process.
	go func() { _ = cmd.Wait() }()

	return pid, nil
}

// underTest reports whether this binary is a `go test` binary. The test flags
// are registered by the generated test main, so their presence is the signal;
// importing testing from production code is not.
func underTest() bool {
	return flag.Lookup("test.v") != nil
}
