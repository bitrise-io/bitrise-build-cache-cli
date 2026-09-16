package common

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// ProjectModeFlagName is the CLI flag every activate subcommand exposes to
// override the machine-wide project scoping mode.
const ProjectModeFlagName = "project-mode"

// ProjectModeFlagUsage is the shared --project-mode help text so every activate
// subcommand documents the flag the same way.
const ProjectModeFlagUsage = "Project scoping mode ('always' or 'opt-in'). " +
	"'always' activates cache for every project; 'opt-in' only activates when a " +
	".bitrise-build-cache.json marker is found walking up from the build's CWD. " +
	"Empty keeps the machine-wide setting; setting a value updates and persists it."

// ResolveAndPersistProjectMode reads the machine-wide config, resolves the
// effective mode from an optional explicit flag value, and persists the value
// back when the flag was set. Returns the effective mode the caller should
// bake into the tool's activation artifact.
func ResolveAndPersistProjectMode(flag string, logger log.Logger) (machineconfig.Mode, error) {
	if err := machineconfig.ValidateFlag(flag); err != nil {
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

	effective := machineconfig.Effective(flag, cfg.ProjectMode)

	if flag != "" && cfg.ProjectMode != effective {
		cfg.ProjectMode = effective
		if err := machineconfig.Write(cfg, osProxy, p); err != nil {
			return "", fmt.Errorf("persist machine config: %w", err)
		}
		if logger != nil {
			logger.TInfof("Machine-wide project scoping is now %q.", string(effective))
		}
	}

	return effective, nil
}
