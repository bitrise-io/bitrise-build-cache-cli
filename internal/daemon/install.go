package daemon

import (
	"context"
	"fmt"
)

// ServiceStatus describes what install/uninstall did for one service.
type ServiceStatus struct {
	Service    Service
	ConfigPath string
	Wrote      bool
	Removed    bool
}

// InstallResult is the per-service outcome returned by Install.
type InstallResult struct {
	BackendName string
	Statuses    []ServiceStatus
}

// UninstallResult is the per-service outcome returned by Uninstall.
type UninstallResult struct {
	BackendName string
	Statuses    []ServiceStatus
}

// Install registers every service with the OS supervisor using the supplied
// Backend. Idempotent — see Backend.Install for the per-service contract.
func Install(ctx context.Context, backend Backend, paths Paths, services []Service, executable string) (InstallResult, error) {
	result := InstallResult{
		BackendName: backend.Name(),
		Statuses:    make([]ServiceStatus, 0, len(services)),
	}

	for _, svc := range services {
		path, err := backend.Install(ctx, paths, svc, executable)
		if err != nil {
			return result, fmt.Errorf("%s install %s: %w", backend.Name(), svc.Name, err)
		}

		result.Statuses = append(result.Statuses, ServiceStatus{
			Service:    svc,
			ConfigPath: path,
			Wrote:      true,
		})
	}

	return result, nil
}

// Uninstall removes every service via the supplied Backend. Idempotent —
// missing units / not-loaded services are treated as success.
func Uninstall(ctx context.Context, backend Backend, paths Paths, services []Service) (UninstallResult, error) {
	result := UninstallResult{
		BackendName: backend.Name(),
		Statuses:    make([]ServiceStatus, 0, len(services)),
	}

	for _, svc := range services {
		path, removed, err := backend.Uninstall(ctx, paths, svc)
		if err != nil {
			return result, fmt.Errorf("%s uninstall %s: %w", backend.Name(), svc.Name, err)
		}

		result.Statuses = append(result.Statuses, ServiceStatus{
			Service:    svc,
			ConfigPath: path,
			Removed:    removed,
		})
	}

	return result, nil
}
