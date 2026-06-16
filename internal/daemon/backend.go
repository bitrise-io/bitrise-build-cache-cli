package daemon

import (
	"context"
	"errors"
	"runtime"
)

var ErrUnsupportedPlatform = errors.New("daemon install is only supported on macOS (launchd) and Linux (systemd)")

type Backend interface {
	Install(ctx context.Context, paths Paths, svc Service, executable string) (configPath string, err error)
	Uninstall(ctx context.Context, paths Paths, svc Service) (configPath string, removed bool, err error)
	Name() string
}

// CommandRunner: exitCode -1 is reserved for "command not started".
type CommandRunner interface {
	Run(ctx context.Context, bin string, args ...string) (stdout string, stderr string, exitCode int, err error)
}

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
