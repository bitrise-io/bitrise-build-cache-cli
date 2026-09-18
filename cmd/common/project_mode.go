package common

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const ProjectModeFlagName = "project-mode"

const ProjectModeFlagUsage = "Project scoping mode ('always' or 'opt-in'). " +
	"'always' activates cache for every project; 'opt-in' only activates when a " +
	".bitrise-build-cache.json marker is found walking up from the build's CWD. " +
	"Empty keeps the machine-wide setting; setting a value updates and persists it."

func ResolveAndPersistProjectMode(flag string, logger log.Logger) (machineconfig.Mode, error) {
	if err := machineconfig.ValidateProjectModeFlag(flag); err != nil {
		return "", fmt.Errorf("--%s: %w", ProjectModeFlagName, err)
	}

	p, err := paths.Default()
	if err != nil {
		return "", fmt.Errorf("resolve home dir for machine config: %w", err)
	}

	osProxy := utils.DefaultOsProxy{}
	cfg, err := machineconfig.Read(osProxy, p, logger)
	if err != nil {
		if logger != nil {
			logger.Warnf("Falling back to 'always' project scoping (%v).", err)
		}
		cfg = machineconfig.Config{}
	}

	effective, err := machineconfig.EffectiveProjectMode(flag, cfg.ProjectMode)
	if err != nil {
		return "", fmt.Errorf("--%s: %w", ProjectModeFlagName, err)
	}

	if flag != "" && cfg.ProjectMode != effective {
		cfg.ProjectMode = effective
		if err := machineconfig.Write(cfg, osProxy, p); err != nil {
			return "", fmt.Errorf("persist machine config: %w", err)
		}
		if logger != nil {
			logger.TInfof("Machine-wide project scoping is now %q.", string(effective))
		}
	}

	ensureProjectMarkerAtCwd(effective, osProxy, logger)

	return effective, nil
}

// ensureProjectMarkerAtCwd writes the per-project marker under cwd when opt-in
// mode is active and no ancestor marker already covers the directory. Failures
// are swallowed by EnsureMarker — a machine without cwd (or a read-only fs)
// still gets a successful activation.
func ensureProjectMarkerAtCwd(mode machineconfig.Mode, osProxy utils.OsProxy, logger log.Logger) {
	cwd, err := osProxy.Getwd()
	if err != nil {
		if logger != nil {
			logger.Debugf("Skipping per-project marker creation: %s", err)
		}

		return
	}
	_ = machineconfig.EnsureMarker(cwd, mode, osProxy, logger)
}
