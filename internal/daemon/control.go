package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
)

var ErrNotInstalled = errors.New("daemon is not installed; run `bitrise-build-cache daemon install` first")

type ControlStatus struct {
	Service    Service
	ConfigPath string
}

type ControlResult struct {
	BackendName string
	Statuses    []ControlStatus
}

func Up(ctx context.Context, backend Backend, paths Paths, services []Service) (ControlResult, error) {
	result := ControlResult{
		BackendName: backend.Name(),
		Statuses:    make([]ControlStatus, 0, len(services)),
	}

	for _, svc := range services {
		path := configPath(backend, paths, svc)

		// Stat-then-Start has a small TOCTOU window with `daemon uninstall`; accepted to keep the friendly ErrNotInstalled.
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return result, fmt.Errorf("%w (missing %s)", ErrNotInstalled, path)
			}

			return result, fmt.Errorf("stat %s: %w", path, err)
		}

		if err := backend.Start(ctx, paths, svc); err != nil {
			return result, fmt.Errorf("%s start %s: %w", backend.Name(), svc.Name, err)
		}

		result.Statuses = append(result.Statuses, ControlStatus{Service: svc, ConfigPath: path})
	}

	return result, nil
}

func Down(ctx context.Context, backend Backend, paths Paths, services []Service) (ControlResult, error) {
	result := ControlResult{
		BackendName: backend.Name(),
		Statuses:    make([]ControlStatus, 0, len(services)),
	}

	for _, svc := range services {
		if err := backend.Stop(ctx, paths, svc); err != nil {
			return result, fmt.Errorf("%s stop %s: %w", backend.Name(), svc.Name, err)
		}

		result.Statuses = append(result.Statuses, ControlStatus{
			Service:    svc,
			ConfigPath: configPath(backend, paths, svc),
		})
	}

	return result, nil
}

// Restart wraps an Up failure that follows a successful Down so the user knows services are left stopped.
func Restart(ctx context.Context, backend Backend, paths Paths, services []Service) (ControlResult, error) {
	if _, err := Down(ctx, backend, paths, services); err != nil {
		return ControlResult{}, err
	}

	result, err := Up(ctx, backend, paths, services)
	if err != nil {
		return result, fmt.Errorf("restart left services stopped (Down succeeded, Up failed); run `bitrise-build-cache daemon up` after fixing: %w", err)
	}

	return result, nil
}

func configPath(backend Backend, paths Paths, svc Service) string {
	switch backend.Name() {
	case "launchd":
		return paths.PlistPath(svc.Label())
	case "systemd":
		return paths.UnitPath(svc.UnitName())
	default:
		return ""
	}
}
