package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const launchctlBin = "/bin/launchctl"

type ExecRunner struct{}

// Run pins LC_ALL=C / LANG=C so supervisor error strings stay English — our substring matches in systemd.go depend on it.
func (ExecRunner) Run(ctx context.Context, bin string, args ...string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.String(), stderr.String(), exitErr.ExitCode(), nil
		}

		return stdout.String(), stderr.String(), -1, fmt.Errorf("run %s: %w", bin, err)
	}

	return stdout.String(), stderr.String(), 0, nil
}

type LaunchdBackend struct {
	Runner CommandRunner
}

func (LaunchdBackend) Name() string { return "launchd" }

func (b LaunchdBackend) Install(ctx context.Context, paths Paths, svc Service, executable string) (string, error) {
	if err := os.MkdirAll(paths.LaunchAgentsDir(), 0o755); err != nil {
		return "", fmt.Errorf("create LaunchAgents dir: %w", err)
	}

	if err := os.MkdirAll(paths.LogDir(), 0o755); err != nil {
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

// bootstrap pre-boots out so a rerun picks up the new executable path on CLI upgrades.
func (b LaunchdBackend) bootstrap(ctx context.Context, plistPath string) error {
	if _, _, _, runErr := b.Runner.Run(ctx, launchctlBin, "bootout", guiTarget(), plistPath); runErr != nil {
		return fmt.Errorf("launchctl bootout (pre-bootstrap): %w", runErr)
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
