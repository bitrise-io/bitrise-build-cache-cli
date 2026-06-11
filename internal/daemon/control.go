package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// ErrNotInstalled is returned by Up when the supervisor config file is not on
// disk. `daemon up` requires `daemon install` to have run at least once — Up
// is for restoring the running state, not creating it.
var ErrNotInstalled = errors.New("daemon is not installed; run `bitrise-build-cache daemon install` first")

// ControlStatus describes the per-service outcome of Up/Down.
type ControlStatus struct {
	Service    Service
	ConfigPath string
}

// ControlResult is the aggregate outcome.
type ControlResult struct {
	BackendName string
	Statuses    []ControlStatus
}

// Up starts every service. Returns ErrNotInstalled if any service's
// supervisor config is missing from disk — the caller is expected to run
// `daemon install` first.
func Up(ctx context.Context, backend Backend, paths Paths, services []Service) (ControlResult, error) {
	result := ControlResult{
		BackendName: backend.Name(),
		Statuses:    make([]ControlStatus, 0, len(services)),
	}

	for _, svc := range services {
		path := configPath(backend, paths, svc)
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

// Down stops every service. Does not remove supervisor config files. Safe to
// run when nothing is registered — Backend.Stop swallows the supervisor's
// "not loaded" exit code.
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

// Restart stops then starts. If Up fails after Down succeeded, the user is
// left with stopped services — we wrap the Up error so the caller knows the
// state isn't "as you found it" and includes the exact remediation command.
//
// If Down itself fails, the services are likely still running, so the error
// surfaces as-is.
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

// configPath resolves the supervisor config file path for svc. The launchd
// and systemd backends use different naming schemes (Label vs UnitName), so
// we dispatch by backend name.
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
