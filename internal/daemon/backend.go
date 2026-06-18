package daemon

import (
	"context"
	"errors"
	"runtime"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/exec"
)

var ErrUnsupportedPlatform = errors.New("daemon install is only supported on macOS (launchd) and Linux (systemd)")

type Backend interface {
	Install(ctx context.Context, paths Paths, svc Service, executable string) (configPath string, err error)
	Uninstall(ctx context.Context, paths Paths, svc Service) (configPath string, removed bool, err error)
	Name() string
}

// CommandRunner aliases the shared exec.Runner for backwards compatibility.
type CommandRunner = exec.Runner

// daemonRunner pins LC_ALL=C/LANG=C so supervisor error strings stay English — substring matches in systemd.go depend on it.
//
//nolint:gochecknoglobals
var daemonRunner = exec.ExecRunner{PinLocale: true}

func DefaultBackend() (Backend, error) {
	switch runtime.GOOS {
	case "darwin":
		return LaunchdBackend{Runner: daemonRunner}, nil
	case "linux":
		return SystemdBackend{Runner: daemonRunner}, nil
	default:
		return nil, ErrUnsupportedPlatform
	}
}
