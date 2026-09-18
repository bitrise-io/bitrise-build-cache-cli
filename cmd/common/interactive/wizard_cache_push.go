package interactive

import (
	"github.com/bitrise-io/go-utils/v2/log"

	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// storedCachePush seeds the wizard's push toggle from the machine config so a
// user who has previously answered doesn't have to re-answer. Any read failure
// falls back to the shared default — the wizard is not the place to surface
// machine config errors.
func storedCachePush(logger log.Logger) bool {
	p, err := paths.Default()
	if err != nil {
		return machineconfig.DefaultCachePush
	}

	got, err := machineconfig.StoredCachePush(utils.DefaultOsProxy{}, p, logger)
	if err != nil {
		return machineconfig.DefaultCachePush
	}

	return got
}

// persistCachePush stores the wizard's chosen push value so future
// non-interactive activations pick it up. Failures are best-effort: the
// wizard already used the value for this run.
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
	if cfg.CachePush != nil && *cfg.CachePush == pushEnabled {
		return
	}

	stored := pushEnabled
	cfg.CachePush = &stored
	if err := machineconfig.Write(cfg, osProxy, p); err != nil {
		if logger != nil {
			logger.Warnf("Could not persist cache push choice (%v). Continuing with %t for this run.", err, pushEnabled)
		}
	}
}
