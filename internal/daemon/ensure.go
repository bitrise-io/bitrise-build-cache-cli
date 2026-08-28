package daemon

import (
	"context"
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/v2/log"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
)

// EnvSkipEnsure suppresses the post-activate Ensure. Callers that own their
// own daemon lifecycle set it to avoid fighting the auto-ensure.
const EnvSkipEnsure = "BITRISE_BUILD_CACHE_SKIP_DAEMON_ENSURE"

// EnsureDeps carries optional test seams; the zero value uses production defaults.
type EnsureDeps struct {
	Envs              map[string]string
	BootstrapFn       func(ctx context.Context, logger log.Logger, services []Service) (InstallResult, Paths, error)
	RestartFn         func(ctx context.Context, backend Backend, paths Paths, services []Service) (ControlResult, error)
	BackendAndPathsFn func() (Backend, Paths, error)
}

// Ensure reconciles each service to running with the just-saved config:
// no config file → Bootstrap; config present → Restart (idempotent Stop+Start).
// EnvSkipEnsure=1 short-circuits the whole batch.
func Ensure(ctx context.Context, logger log.Logger, services []Service, deps EnsureDeps) error {
	envs := deps.Envs
	if envs == nil {
		envs = map[string]string{}
	}

	if envs[EnvSkipEnsure] != "" || os.Getenv(EnvSkipEnsure) != "" {
		return nil
	}

	// A supervised proxy is for a developer machine, where it has to outlive the
	// shell that started it. On CI the build owns the whole VM and the wrapper can
	// start the proxy itself, which is also the only configuration measured to keep
	// cache operations fast under a parallel build.
	if provider := configcommon.DetectCIProvider(allEnvs(envs)); provider != "" {
		logger.Debugf("CI provider %s detected, leaving the proxy to the build tool wrapper", provider)

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

// allEnvs merges the process environment under the caller-supplied map, so CI
// detection sees the same variables whether or not the caller passed them in.
func allEnvs(envs map[string]string) map[string]string {
	merged := make(map[string]string, len(envs)+4)
	for _, key := range configcommon.CIProviderEnvKeys() {
		if v := os.Getenv(key); v != "" {
			merged[key] = v
		}
	}
	for k, v := range envs {
		merged[k] = v
	}

	return merged
}
