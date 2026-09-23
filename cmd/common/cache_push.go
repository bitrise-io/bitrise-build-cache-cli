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

// ResolveAndPersistCachePush picks the effective --cache-push value and, when
// the caller set the flag explicitly, persists it to the machine config.
// cmd.Flags().Changed distinguishes "user typed --cache-push" from "flag holds
// its default value" — the flag is per-command, so cmd owns its definition.
func ResolveAndPersistCachePush(cmd *cobra.Command, flagValue bool, logger log.Logger) (bool, error) {
	changed := cmd != nil && cmd.Flags().Changed(CachePushFlagName)

	p, err := paths.Default()
	if err != nil {
		return machineconfig.DefaultCachePush, fmt.Errorf("resolve home dir for machine config: %w", err)
	}

	osProxy := utils.DefaultOsProxy{}
	current, err := machineconfig.Read(osProxy, p, logger)
	if err != nil {
		if logger != nil {
			logger.Warnf("Falling back to default cache push (%v).", err)
		}
		current = machineconfig.Config{}
	}

	if !changed {
		return machineconfig.ResolvedCachePush(current), nil
	}

	push := flagValue

	if err := PersistCachePush(current, push, osProxy, p); err != nil {
		return push, err
	}
	if logger != nil {
		logger.TInfof("Machine-wide cache push is now %t.", push)
	}

	return push, nil
}

// PersistCachePush writes cfg with CachePush=effective, no-op when the stored
// value already matches — a redundant activate must not churn the file's mtime.
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
