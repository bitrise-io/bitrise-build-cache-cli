package daemon

import (
	"context"
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/v2/log"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// EnvSkipEnsure suppresses the post-activate Ensure. Callers that own their
// own daemon lifecycle set it to avoid fighting the auto-ensure.
const EnvSkipEnsure = "BITRISE_BUILD_CACHE_SKIP_DAEMON_ENSURE"

// EnsureDeps carries optional test seams; the zero value uses production defaults.
type EnsureDeps struct {
	Envs              map[string]string
	DebugLogging      bool
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

	// Reverted to this after v3.6.7 re-enabled the daemon on CI: the reference
	// app's builds got slower, so the launchd proxy is still not the right
	// lifecycle here. ProcessType Interactive fixed the timeouts, not the
	// slowdown. The proxy exists for developer machines, where it has to
	// outlive the shell that started it; nothing on CI needs it to persist.
	if provider := configcommon.DetectCIProvider(mergedEnvs(envs)); provider != "" {
		logger.Debugf("CI provider %s detected, leaving the proxy to the build tool wrapper", provider)

		return nil
	}

	if len(services) == 0 {
		return nil
	}

	if deps.DebugLogging {
		services = WithDebugLogging(services)
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

// mergedEnvs lets the caller's map win over the process environment, so a test
// or a caller that passes envs explicitly is not overridden by the ambient one.
func mergedEnvs(envs map[string]string) map[string]string {
	merged := utils.AllEnvs()
	for k, v := range envs {
		merged[k] = v
	}

	return merged
}
