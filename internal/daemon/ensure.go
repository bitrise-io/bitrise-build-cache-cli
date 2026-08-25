package daemon

import (
	"context"
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/v2/log"
)

// EnvSkipEnsure suppresses the post-activate daemon Ensure. Set by callers
// (e.g. the interactive wizard) that own their own daemon lifecycle so we
// don't double-install or override their user-driven answer.
const EnvSkipEnsure = "BITRISE_BUILD_CACHE_SKIP_DAEMON_ENSURE"

// EnsureDeps holds the seams Ensure needs to make decisions and act. All fields
// are optional; the zero value uses production defaults.
type EnsureDeps struct {
	// Envs — for reading EnvSkipEnsure. Defaults to os.Getenv lookup.
	Envs map[string]string
	// Probe resolves each service's socket path and probes it. Required in
	// tests; Ensure without a Probe assumes ProbeStopped, which forces
	// Bootstrap-or-Up (never Restart) — safe fallback.
	Probe ProbeFn
	// BootstrapFn overrides the production install-and-start path. Injected in
	// tests. Defaults to the real Bootstrap.
	BootstrapFn func(ctx context.Context, logger log.Logger, services []Service) (InstallResult, Paths, error)
	// UpFn / RestartFn are indirected the same way so the state machine can be
	// exercised without a real supervisor.
	UpFn      func(ctx context.Context, backend Backend, paths Paths, services []Service) (ControlResult, error)
	RestartFn func(ctx context.Context, backend Backend, paths Paths, services []Service) (ControlResult, error)
	// BackendAndPathsFn resolves the backend + paths to use for Up / Restart.
	// Defaults to DefaultBackendAndPaths so tests can inject a fake backend
	// without touching launchctl / systemctl.
	BackendAndPathsFn func() (Backend, Paths, error)
}

// Ensure guarantees each service ends up running with the just-saved config.
//
// Decision matrix, per service:
//
//	| Config file exists | Running? | pushChanged | Action        |
//	|--------------------|----------|-------------|---------------|
//	| no                 | —        | —           | Bootstrap     |
//	| yes                | no       | —           | Up            |
//	| yes                | yes      | no          | Noop          |
//	| yes                | yes      | yes         | Restart       |
//
// pushChanged is computed by the caller (they own the old→new config diff).
//
// Setting EnvSkipEnsure=1 short-circuits the whole batch — the wizard uses this
// so its explicit user-driven daemon prompt wins.
func Ensure(ctx context.Context, logger log.Logger, services []Service, pushChanged bool, deps EnsureDeps) error {
	envs := deps.Envs
	if envs == nil {
		envs = map[string]string{}
	}

	if envs[EnvSkipEnsure] != "" || os.Getenv(EnvSkipEnsure) != "" {
		return nil
	}

	if len(services) == 0 {
		return nil
	}

	backendAndPaths := deps.BackendAndPathsFn
	if backendAndPaths == nil {
		backendAndPaths = DefaultBackendAndPaths
	}

	backend, dPaths, err := backendAndPaths()
	if err != nil {
		return err
	}

	probe := deps.Probe
	if probe == nil {
		probe = func(context.Context, Service) SocketProbe { return ProbeStopped }
	}

	bootstrapFn := deps.BootstrapFn
	if bootstrapFn == nil {
		bootstrapFn = Bootstrap
	}

	upFn := deps.UpFn
	if upFn == nil {
		upFn = Up
	}

	restartFn := deps.RestartFn
	if restartFn == nil {
		restartFn = Restart
	}

	for _, svc := range services {
		if err := ensureOne(ctx, logger, backend, dPaths, svc, pushChanged, probe, bootstrapFn, upFn, restartFn); err != nil {
			return err
		}
	}

	return nil
}

func ensureOne(
	ctx context.Context,
	logger log.Logger,
	backend Backend,
	dPaths Paths,
	svc Service,
	pushChanged bool,
	probe ProbeFn,
	bootstrapFn func(context.Context, log.Logger, []Service) (InstallResult, Paths, error),
	upFn func(context.Context, Backend, Paths, []Service) (ControlResult, error),
	restartFn func(context.Context, Backend, Paths, []Service) (ControlResult, error),
) error {
	one := []Service{svc}
	cfg := configPath(backend, dPaths, svc)

	if _, err := os.Stat(cfg); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat %s: %w", cfg, err)
		}

		if _, _, err := bootstrapFn(ctx, logger, one); err != nil {
			return fmt.Errorf("bootstrap %s: %w", svc.Name, err)
		}

		return nil
	}

	switch {
	case probe(ctx, svc) != ProbeRunning:
		if _, err := upFn(ctx, backend, dPaths, one); err != nil {
			return fmt.Errorf("start %s: %w", svc.Name, err)
		}

		return nil
	case pushChanged:
		if _, err := restartFn(ctx, backend, dPaths, one); err != nil {
			return fmt.Errorf("restart %s: %w", svc.Name, err)
		}

		return nil
	default:
		return nil
	}
}
