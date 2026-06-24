package daemon

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const launchctlBin = "/bin/launchctl"

type LaunchdBackend struct {
	Runner CommandRunner
}

func (LaunchdBackend) Name() string { return "launchd" }

func (b LaunchdBackend) Install(ctx context.Context, paths Paths, svc Service, executable string) (string, error) {
	if err := os.MkdirAll(paths.LaunchAgentsDir(), 0o755); err != nil {
		return "", fmt.Errorf("create LaunchAgents dir: %w", err)
	}

	if err := os.MkdirAll(paths.DaemonLogDir(), 0o755); err != nil {
		return "", fmt.Errorf("create log dir: %w", err)
	}

	plist, err := GeneratePlist(svc, executable, paths)
	if err != nil {
		return "", fmt.Errorf("generate plist for %s: %w", svc.Name, err)
	}

	path := paths.PlistPath(svc.Label())
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil { //nolint:gosec // plist must be world-readable for launchctl
		return path, fmt.Errorf("write plist %s: %w", path, err)
	}

	if err := b.bootstrap(ctx, path); err != nil {
		return path, err
	}

	return path, nil
}

func (b LaunchdBackend) Start(ctx context.Context, paths Paths, svc Service) error {
	path := paths.PlistPath(svc.Label())

	return b.bootstrap(ctx, path)
}

func (b LaunchdBackend) Stop(ctx context.Context, paths Paths, svc Service) error {
	path := paths.PlistPath(svc.Label())

	return b.bootout(ctx, path)
}

func (b LaunchdBackend) Uninstall(ctx context.Context, paths Paths, svc Service) (string, bool, error) {
	path := paths.PlistPath(svc.Label())

	if err := b.bootout(ctx, path); err != nil {
		return path, false, err
	}

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return path, false, nil
		}

		return path, false, fmt.Errorf("remove plist %s: %w", path, err)
	}

	return path, true, nil
}

func guiTarget() string {
	return "gui/" + strconv.Itoa(os.Getuid())
}

const launchctlBootoutNotLoaded = 5

// bootstrap pre-boots out so a rerun picks up the new executable path on CLI upgrades.
func (b LaunchdBackend) bootstrap(ctx context.Context, plistPath string) error {
	_, stderr, code, runErr := b.Runner.Run(ctx, launchctlBin, "bootout", guiTarget(), plistPath)
	if runErr != nil {
		return fmt.Errorf("launchctl bootout (pre-bootstrap): %w", runErr)
	}

	if code != 0 && code != launchctlBootoutNotLoaded {
		return fmt.Errorf("launchctl bootout (pre-bootstrap) exited %d: %s", code, strings.TrimSpace(stderr))
	}

	_, stderr, code, err := b.Runner.Run(ctx, launchctlBin, "bootstrap", guiTarget(), plistPath)
	if err != nil {
		return fmt.Errorf("launchctl bootstrap: %w", err)
	}

	if code != 0 {
		return fmt.Errorf("launchctl bootstrap %s exited %d: %s", plistPath, code, strings.TrimSpace(stderr))
	}

	return nil
}

// bootout treats exit 5 ("no such service") as success.
func (b LaunchdBackend) bootout(ctx context.Context, plistPath string) error {
	_, stderr, code, err := b.Runner.Run(ctx, launchctlBin, "bootout", guiTarget(), plistPath)
	if err != nil {
		return fmt.Errorf("launchctl bootout: %w", err)
	}

	if code != 0 && code != 5 {
		return fmt.Errorf("launchctl bootout %s exited %d: %s", plistPath, code, strings.TrimSpace(stderr))
	}

	return nil
}
