package common

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

const CachePushFlagName = "cache-push"

// ResolveAndPersistCachePush picks the effective --cache-push value and, if
// the caller set the flag explicitly, persists it to the machine config so
// later activations without the flag pick the same choice.
//
// The flag is per-command (each activate command registers its own), so the
// caller passes the current flag value and the cobra command that owns the
// flag definition — cmd.Flags().Changed(name) is the source of truth for
// "did the user actually type --cache-push?" as opposed to "the flag has its
// default value".
func ResolveAndPersistCachePush(cmd *cobra.Command, flagValue bool, logger log.Logger) (bool, error) {
	changed := cmd != nil && cmd.Flags().Changed(CachePushFlagName)

	p, err := paths.Default()
	if err != nil {
		return machineconfig.DefaultCachePush, fmt.Errorf("resolve home dir for machine config: %w", err)
	}

	osProxy := utils.DefaultOsProxy{}
	cfg, err := machineconfig.Read(osProxy, p, logger)
	if err != nil {
		if logger != nil {
			logger.Warnf("Falling back to default cache push (%v).", err)
		}
		cfg = machineconfig.Config{}
	}

	effective := machineconfig.EffectiveCachePush(changed, flagValue, cfg.CachePush)

	if !changed {
		return effective, nil
	}

	if err := PersistCachePush(cfg, effective, osProxy, p); err != nil {
		return effective, err
	}
	if logger != nil {
		logger.TInfof("Machine-wide cache push is now %t.", effective)
	}

	return effective, nil
}

// PersistCachePush writes cfg back to disk with CachePush=effective, but only
// when the value actually changed — a no-op when the stored value already
// matches, so a redundant activate does not churn the file's mtime. Both the
// CLI resolver and the interactive wizard funnel through this single writer.
func PersistCachePush(cfg machineconfig.Config, effective bool, osProxy utils.OsProxy, p paths.Paths) error {
	if cfg.CachePush != nil && *cfg.CachePush == effective {
		return nil
	}

	stored := effective
	cfg.CachePush = &stored
	if err := machineconfig.Write(cfg, osProxy, p); err != nil {
		return fmt.Errorf("persist machine config: %w", err)
	}

	return nil
}
