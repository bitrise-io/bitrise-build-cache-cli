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

// PersistProjectMode validates flag and writes it into the machine config when
// non-empty. Empty flag is a no-op.
func PersistProjectMode(flag string, logger log.Logger) error {
	if flag == "" {
		return nil
	}

	if err := machineconfig.ValidateProjectMode(flag); err != nil {
		return fmt.Errorf("--%s: %w", ProjectModeFlagName, err)
	}

	p, err := paths.Default()
	if err != nil {
		return fmt.Errorf("resolve home dir for machine config: %w", err)
	}

	osProxy := utils.DefaultOsProxy{}
	current, err := machineconfig.Read(osProxy, p, logger)
	if err != nil {
		if logger != nil {
			logger.Warnf("Overwriting unreadable machine config (%v).", err)
		}
		current = machineconfig.Config{}
	}

	mode := machineconfig.Mode(flag)
	if current.ProjectMode == mode {
		return nil
	}

	current.ProjectMode = mode
	if err := machineconfig.Write(current, osProxy, p); err != nil {
		return fmt.Errorf("persist machine config: %w", err)
	}
	if logger != nil {
		logger.TInfof("Machine-wide project scoping is now %q.", string(mode))
	}

	return nil
}
