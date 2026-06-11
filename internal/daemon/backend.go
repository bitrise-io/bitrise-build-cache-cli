package daemon

import (
	"context"
	"errors"
	"runtime"
)

// ErrUnsupportedPlatform is returned by DefaultBackend on hosts that aren't
// macOS or Linux. The CLI is not used on Windows today; the sentinel exists so
// callers can surface a friendly message rather than a generic error.
var ErrUnsupportedPlatform = errors.New("daemon install is only supported on macOS (launchd) and Linux (systemd)")

// Backend installs / uninstalls one service with the host OS's user-level
// supervisor (launchd on macOS, systemd --user on Linux). Implementations are
// stateless aside from the runner they hold.
type Backend interface {
	// Install writes the supervisor config for svc and starts it. Returns the
	// path of the written config file (for reporting).
	Install(ctx context.Context, paths Paths, svc Service, executable string) (configPath string, err error)
	// Uninstall stops the service and removes its supervisor config. Returns
	// the config path and whether a file was actually removed (false = already
	// absent, which is treated as success).
	Uninstall(ctx context.Context, paths Paths, svc Service) (configPath string, removed bool, err error)
	// Name is the supervisor's short name ("launchd" / "systemd"). Used in
	// human-readable output.
	Name() string
}

// CommandRunner abstracts the OS-level supervisor CLI (launchctl / systemctl)
// so tests can replay deterministic invocations without touching the real
// supervisor. exitCode of -1 is reserved for "command not started" — every
// non-error completion returns the process exit code, including non-zero.
type CommandRunner interface {
	Run(ctx context.Context, bin string, args ...string) (stdout string, stderr string, exitCode int, err error)
}

// DefaultBackend picks the right backend for the current host. Uses ExecRunner
// for command execution.
func DefaultBackend() (Backend, error) {
	switch runtime.GOOS {
	case "darwin":
		return LaunchdBackend{Runner: ExecRunner{}}, nil
	case "linux":
		return SystemdBackend{Runner: ExecRunner{}}, nil
	default:
		return nil, ErrUnsupportedPlatform
	}
}
