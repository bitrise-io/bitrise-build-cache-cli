package daemon

import (
	"context"
	"fmt"
	"os"
	"strings"
)

const systemctlBin = "/usr/bin/systemctl"

// SystemdBackend installs services as per-user systemd units under
// ~/.config/systemd/user. Requires `systemctl --user` to be reachable — i.e.
// the user has a systemd session (loginctl enable-linger for headless boxes).
type SystemdBackend struct {
	Runner CommandRunner
}

// Name implements Backend.
func (SystemdBackend) Name() string { return "systemd" }

// Install writes a unit file and enables it with `systemctl --user enable
// --now <unit>`. Rerun is safe: the unit file is overwritten, daemon-reload
// re-reads it, and enable --now is idempotent.
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

// Start enables and starts the unit. Same verb as Install minus the unit
// file write — assumes the file is already on disk. Idempotent (enable --now
// on a running unit is a no-op).
func (b SystemdBackend) Start(ctx context.Context, _ Paths, svc Service) error {
	if err := b.daemonReload(ctx); err != nil {
		return err
	}

	return b.enableNow(ctx, svc.UnitName())
}

// Stop stops the unit without disabling it — the unit will come back on next
// login. Use Uninstall for permanent removal.
func (b SystemdBackend) Stop(ctx context.Context, _ Paths, svc Service) error {
	return b.stop(ctx, svc.UnitName())
}

// Uninstall disables + stops the service and removes its unit file. Missing
// unit / not-loaded service is success.
func (b SystemdBackend) Uninstall(ctx context.Context, paths Paths, svc Service) (string, bool, error) {
	path := paths.UnitPath(svc.UnitName())

	// disable --now stops the unit and removes any enabled symlinks. Non-zero
	// exit on a never-enabled unit is fine — we still want to remove the file.
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

	// Reload so systemd forgets about the deleted file. Best-effort: if the
	// removal succeeded, we're already in a coherent state.
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

// stop runs `systemctl --user stop <unit>`. A stop against a not-loaded unit
// exits non-zero with "Unit ... not loaded" — treated as success so Stop is
// idempotent. Unit stays enabled, so it will come back on next login.
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

func (b SystemdBackend) disableNow(ctx context.Context, unitName string) error {
	// `disable --now` of a non-existent unit exits 1 with "Failed to disable
	// unit: Unit file <name>.service does not exist." — we treat that as
	// success so uninstall is idempotent.
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
