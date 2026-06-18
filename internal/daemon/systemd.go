package daemon

import (
	"context"
	"fmt"
	"os"
	"strings"
)

const systemctlBin = "/usr/bin/systemctl"

// SystemdBackend requires `systemctl --user` to be reachable (loginctl enable-linger on headless boxes).
type SystemdBackend struct {
	Runner CommandRunner
}

func (SystemdBackend) Name() string { return "systemd" }

func (b SystemdBackend) Install(ctx context.Context, paths Paths, svc Service, executable string) (string, error) {
	if err := os.MkdirAll(paths.SystemdUserDir(), 0o755); err != nil {
		return "", fmt.Errorf("create systemd user dir: %w", err)
	}

	if err := os.MkdirAll(paths.LogDir(), 0o755); err != nil {
		return "", fmt.Errorf("create log dir: %w", err)
	}

	unit, err := GenerateUnit(svc, executable)
	if err != nil {
		return "", fmt.Errorf("generate unit for %s: %w", svc.Name, err)
	}

	path := paths.UnitPath(svc.UnitName())
	if err := os.WriteFile(path, []byte(unit), 0o644); err != nil { //nolint:gosec // unit must be readable by systemd
		return path, fmt.Errorf("write unit %s: %w", path, err)
	}

	if err := b.daemonReload(ctx); err != nil {
		return path, err
	}

	if err := b.enableNow(ctx, svc.UnitName()); err != nil {
		return path, err
	}

	return path, nil
}

func (b SystemdBackend) Start(ctx context.Context, _ Paths, svc Service) error {
	if err := b.daemonReload(ctx); err != nil {
		return err
	}

	return b.enableNow(ctx, svc.UnitName())
}

func (b SystemdBackend) Stop(ctx context.Context, _ Paths, svc Service) error {
	return b.stop(ctx, svc.UnitName())
}

func (b SystemdBackend) Uninstall(ctx context.Context, paths Paths, svc Service) (string, bool, error) {
	path := paths.UnitPath(svc.UnitName())

	if err := b.disableNow(ctx, svc.UnitName()); err != nil {
		return path, false, err
	}

	removed := false
	if err := os.Remove(path); err != nil {
		if !os.IsNotExist(err) {
			return path, false, fmt.Errorf("remove unit %s: %w", path, err)
		}
	} else {
		removed = true
	}

	_ = b.daemonReload(ctx)

	return path, removed, nil
}

func (b SystemdBackend) daemonReload(ctx context.Context) error {
	_, stderr, code, err := b.Runner.Run(ctx, systemctlBin, "--user", "daemon-reload")
	if err != nil {
		return fmt.Errorf("systemctl --user daemon-reload: %w", err)
	}

	if code != 0 {
		return fmt.Errorf("systemctl --user daemon-reload exited %d: %s", code, strings.TrimSpace(stderr))
	}

	return nil
}

func (b SystemdBackend) enableNow(ctx context.Context, unitName string) error {
	_, stderr, code, err := b.Runner.Run(ctx, systemctlBin, "--user", "enable", "--now", unitName+".service")
	if err != nil {
		return fmt.Errorf("systemctl --user enable --now %s: %w", unitName, err)
	}

	if code != 0 {
		return fmt.Errorf("systemctl --user enable --now %s exited %d: %s", unitName, code, strings.TrimSpace(stderr))
	}

	return nil
}

// stop treats "Unit ... not loaded" as success so Stop is idempotent.
func (b SystemdBackend) stop(ctx context.Context, unitName string) error {
	_, stderr, code, err := b.Runner.Run(ctx, systemctlBin, "--user", "stop", unitName+".service")
	if err != nil {
		return fmt.Errorf("systemctl --user stop %s: %w", unitName, err)
	}

	if code == 0 {
		return nil
	}

	combined := strings.TrimSpace(stderr)
	if strings.Contains(combined, "not loaded") || strings.Contains(combined, "does not exist") || strings.Contains(combined, "no such file") {
		return nil
	}

	return fmt.Errorf("systemctl --user stop %s exited %d: %s", unitName, code, combined)
}

// disableNow treats "does not exist" stderr as success so uninstall is idempotent.
func (b SystemdBackend) disableNow(ctx context.Context, unitName string) error {
	_, stderr, code, err := b.Runner.Run(ctx, systemctlBin, "--user", "disable", "--now", unitName+".service")
	if err != nil {
		return fmt.Errorf("systemctl --user disable --now %s: %w", unitName, err)
	}

	if code == 0 {
		return nil
	}

	combined := strings.TrimSpace(stderr)
	if strings.Contains(combined, "does not exist") || strings.Contains(combined, "no such file") {
		return nil
	}

	return fmt.Errorf("systemctl --user disable --now %s exited %d: %s", unitName, code, combined)
}
