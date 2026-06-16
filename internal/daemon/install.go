package daemon

import (
	"context"
	"fmt"
)

type ServiceStatus struct {
	Service    Service
	ConfigPath string
	Wrote      bool
	Removed    bool
}

type InstallResult struct {
	BackendName string
	Statuses    []ServiceStatus
}

type UninstallResult struct {
	BackendName string
	Statuses    []ServiceStatus
}

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
