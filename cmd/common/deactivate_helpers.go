package common

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"

	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/bazel"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/ccache"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/gradle"
	rnconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/reactnative"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
	ccachepkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/ccache"
)

// DeactivateGradle wraps gradleconfig.Deactivate with the same param resolution
// the standalone `deactivate gradle` cmd uses. Shared with `deactivate react-native`
// and `deactivate all`.
func DeactivateGradle(logger log.Logger, dryRun bool) error {
	p, err := paths.Default()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}

	allEnvs := utils.AllEnvs()
	gradleHome := p.GradleHome(allEnvs[paths.GradleUserHomeEnvKey])

	if err := gradleconfig.Deactivate(logger, gradleconfig.DeactivateParams{
		GradleHome: gradleHome,
		Home:       p.Home,
		DryRun:     dryRun,
	}); err != nil {
		return fmt.Errorf("deactivate Gradle: %w", err)
	}

	return nil
}

// DeactivateBazel wraps bazelconfig.Deactivate for the `deactivate all` fan-out.
func DeactivateBazel(logger log.Logger, dryRun bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	bazelrcPath := filepath.Join(home, ".bazelrc")

	if err := bazelconfig.Deactivate(logger, bazelconfig.DeactivateParams{
		BazelrcPath: bazelrcPath,
		Home:        home,
		DryRun:      dryRun,
	}); err != nil {
		return fmt.Errorf("deactivate Bazel: %w", err)
	}

	return nil
}

// DeactivateXcode wraps xcelerate.Deactivate for the RN + `deactivate all` fan-outs.
func DeactivateXcode(logger log.Logger, dryRun bool) error {
	if err := xcelerate.Deactivate(logger, xcelerate.DeactivateParams{
		Envs:   utils.AllEnvs(),
		DryRun: dryRun,
	}); err != nil {
		return fmt.Errorf("deactivate Xcode: %w", err)
	}

	return nil
}

// DeactivateCcache stops the running storage helper (best-effort) then invokes
// ccacheconfig.Deactivate to drop the config file. Shared by the standalone,
// RN and `deactivate all` code paths.
func DeactivateCcache(ctx context.Context, logger log.Logger, dryRun bool) error {
	return DeactivateCcacheWithSocket(ctx, logger, "", dryRun)
}

// DeactivateCcacheWithSocket is DeactivateCcache with an explicit socket path
// override (matches the `--socket` flag on `deactivate ccache`).
func DeactivateCcacheWithSocket(ctx context.Context, logger log.Logger, socketOverride string, dryRun bool) error {
	var errs []error

	envs := utils.AllEnvs()
	osProxy := utils.DefaultOsProxy{}
	socketPath := ccacheconfig.ResolveIPCSocketPath(socketOverride, envs, osProxy)

	if dryRun {
		logger.TInfof("[dry-run] would stop ccache storage helper on %s", socketPath)
	} else if err := ccachepkg.StopStorageHelperAt(ctx, logger, socketPath); err != nil {
		errs = append(errs, fmt.Errorf("stop ccache storage helper: %w", err))
	}

	if err := ccacheconfig.Deactivate(logger, ccacheconfig.DeactivateParams{DryRun: dryRun}); err != nil {
		errs = append(errs, fmt.Errorf("deactivate C++ cache: %w", err))
	}

	return errors.Join(errs...)
}

// DeactivateReactNativeMarker removes only the RN marker file — the tool-specific
// cleanup is invoked separately by the callers so the accumulated error list
// stays granular per-tool.
func DeactivateReactNativeMarker(logger log.Logger, dryRun bool) error {
	if err := rnconfig.Deactivate(logger, rnconfig.DeactivateParams{DryRun: dryRun}); err != nil {
		return fmt.Errorf("deactivate react-native marker: %w", err)
	}

	return nil
}
