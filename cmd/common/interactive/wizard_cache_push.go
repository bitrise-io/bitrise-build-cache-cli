package interactive

import (
	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// storedCachePush seeds the wizard's push toggle from the machine config. Any
// read failure falls back to DefaultCachePush; the wizard is not the place to
// surface machine config errors.
func storedCachePush(logger log.Logger) bool {
	p, err := paths.Default()
	if err != nil {
		return machineconfig.DefaultCachePush
	}

	current, err := machineconfig.Read(utils.DefaultOsProxy{}, p, logger)
	if err != nil {
		return machineconfig.DefaultCachePush
	}

	return machineconfig.ResolvedCachePush(current)
}

// persistCachePush stores the wizard's chosen push value best-effort; the run
// itself already used the value.
func persistCachePush(pushEnabled bool, logger log.Logger) {
	p, err := paths.Default()
	if err != nil {
		if logger != nil {
			logger.Warnf("Could not persist cache push choice (%v). Continuing with %t for this run.", err, pushEnabled)
		}

		return
	}

	osProxy := utils.DefaultOsProxy{}
	cfg, err := machineconfig.Read(osProxy, p, logger)
	if err != nil {
		cfg = machineconfig.Config{}
	}
	if err := common.PersistCachePush(cfg, pushEnabled, osProxy, p); err != nil {
		if logger != nil {
			logger.Warnf("Could not persist cache push choice (%v). Continuing with %t for this run.", err, pushEnabled)
		}
	}
}
