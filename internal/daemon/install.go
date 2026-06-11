package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
)

// ErrUnsupportedPlatform is returned by Install/Uninstall on non-macOS hosts.
// Linux support is tracked in ACI-5031.
var ErrUnsupportedPlatform = errors.New("daemon install is currently macOS-only; Linux support tracked in ACI-5031")

// ServiceStatus describes what install/uninstall did for one service.
type ServiceStatus struct {
	Service   Service
	PlistPath string
	Wrote     bool
	Removed   bool
}

// InstallResult is the per-service outcome returned by Install.
type InstallResult struct {
	Statuses []ServiceStatus
}

// UninstallResult is the per-service outcome returned by Uninstall.
type UninstallResult struct {
	Statuses []ServiceStatus
}

// Install writes plists for every service into ~/Library/LaunchAgents and
// bootstraps them with launchctl. Idempotent: re-running rewrites the plists
// (picks up CLI upgrades) and re-bootstraps.
func Install(ctx context.Context, runner Runner, paths Paths, services []Service, executable string) (InstallResult, error) {
	if runtime.GOOS != "darwin" {
		return InstallResult{}, ErrUnsupportedPlatform
	}

	if err := os.MkdirAll(paths.LaunchAgentsDir(), 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("create LaunchAgents dir: %w", err)
	}

	if err := os.MkdirAll(paths.LogDir(), 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("create log dir: %w", err)
	}

	result := InstallResult{Statuses: make([]ServiceStatus, 0, len(services))}

	for _, svc := range services {
		plist, err := GeneratePlist(svc, executable, paths)
		if err != nil {
			return result, fmt.Errorf("generate plist for %s: %w", svc.Name, err)
		}

		path := paths.PlistPath(svc.Label())
		if err := os.WriteFile(path, []byte(plist), 0o644); err != nil { //nolint:gosec // plist must be world-readable for launchctl
			return result, fmt.Errorf("write plist %s: %w", path, err)
		}

		if err := Bootstrap(ctx, runner, path); err != nil {
			return result, fmt.Errorf("bootstrap %s: %w", svc.Name, err)
		}

		result.Statuses = append(result.Statuses, ServiceStatus{
			Service:   svc,
			PlistPath: path,
			Wrote:     true,
		})
	}

	return result, nil
}

// Uninstall boots out every service and removes its plist. Missing plists /
// not-loaded services are treated as success.
func Uninstall(ctx context.Context, runner Runner, paths Paths, services []Service) (UninstallResult, error) {
	if runtime.GOOS != "darwin" {
		return UninstallResult{}, ErrUnsupportedPlatform
	}

	result := UninstallResult{Statuses: make([]ServiceStatus, 0, len(services))}

	for _, svc := range services {
		path := paths.PlistPath(svc.Label())

		if err := Bootout(ctx, runner, path); err != nil {
			return result, fmt.Errorf("bootout %s: %w", svc.Name, err)
		}

		removed := false
		if err := os.Remove(path); err != nil {
			if !os.IsNotExist(err) {
				return result, fmt.Errorf("remove plist %s: %w", path, err)
			}
		} else {
			removed = true
		}

		result.Statuses = append(result.Statuses, ServiceStatus{
			Service:   svc,
			PlistPath: path,
			Removed:   removed,
		})
	}

	return result, nil
}
