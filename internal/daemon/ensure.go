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
	// BootstrapFn overrides the production install-and-start path. Injected in
	// tests. Defaults to the real Bootstrap.
	BootstrapFn func(ctx context.Context, logger log.Logger, services []Service) (InstallResult, Paths, error)
	// RestartFn is indirected the same way so the state machine can be
	// exercised without a real supervisor.
	RestartFn func(ctx context.Context, backend Backend, paths Paths, services []Service) (ControlResult, error)
	// BackendAndPathsFn resolves the backend + paths to use for Restart.
	// Defaults to DefaultBackendAndPaths so tests can inject a fake backend
	// without touching launchctl / systemctl.
	BackendAndPathsFn func() (Backend, Paths, error)
}

// Ensure guarantees each service ends up running with the just-saved config.
//
// Decision per service:
//
//	| Config file exists | Action    |
//	|--------------------|-----------|
//	| no                 | Bootstrap |
//	| yes                | Restart   |
//
// Restart unconditionally cycles the service so the just-saved config is
// picked up. Both backend Stop implementations are idempotent, so Restart
// works whether the service was previously running or not.
//
// Setting EnvSkipEnsure=1 short-circuits the whole batch — the wizard uses this
// so its explicit user-driven daemon prompt wins.
func Ensure(ctx context.Context, logger log.Logger, services []Service, deps EnsureDeps) error {
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

	bootstrapFn := deps.BootstrapFn
	if bootstrapFn == nil {
		bootstrapFn = Bootstrap
	}

	restartFn := deps.RestartFn
	if restartFn == nil {
		restartFn = Restart
	}

	for _, svc := range services {
		if err := ensureOne(ctx, logger, backend, dPaths, svc, bootstrapFn, restartFn); err != nil {
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
	bootstrapFn func(context.Context, log.Logger, []Service) (InstallResult, Paths, error),
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

	if _, err := restartFn(ctx, backend, dPaths, one); err != nil {
		return fmt.Errorf("restart %s: %w", svc.Name, err)
	}

	return nil
}
