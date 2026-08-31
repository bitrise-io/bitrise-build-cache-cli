package xcelerate

import (
	"errors"
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/envexport"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// XcelerateShellRCBlockName mirrors the name activate uses via envexport.ExportToShellRC.
// Deactivate strips the same block, so keeping this in one place prevents drift.
const XcelerateShellRCBlockName = "Bitrise Xcelerate"

// DeactivateParams controls the xcelerate/Xcode deactivate flow.
type DeactivateParams struct {
	// Envs is the environment map (used by RemoveFromShellRC to locate $HOME).
	Envs map[string]string
	// DryRun logs intended actions instead of performing them.
	DryRun bool
}

// Deactivate undoes what Activate wrote for Xcode:
//   - stops the running xcelerate proxy (best-effort)
//   - deletes ~/.bitrise-xcelerate/ (config + wrapper scripts + copied CLI)
//   - strips the "Bitrise Xcelerate" export block from ~/.bashrc and ~/.zshrc
//
// Logs and enrichment queue under ~/.local/state/xcelerate/* are intentionally
// preserved for debugging. The auth credential store under
// ~/.bitrise/analytics/multiplatform/config.json is owned by auth logout.
func Deactivate(logger log.Logger, params DeactivateParams) error {
	var errs []error

	osProxy := utils.DefaultOsProxy{}

	if err := stopProxyForDeactivate(logger, osProxy, params.DryRun); err != nil {
		errs = append(errs, err)
	}

	if err := removeXcelerateRoot(logger, params.DryRun); err != nil {
		errs = append(errs, err)
	}

	stripShellRC(logger, params.Envs, params.DryRun)

	return errors.Join(errs...)
}

func stopProxyForDeactivate(logger log.Logger, osProxy utils.OsProxy, dryRun bool) error {
	if dryRun {
		if _, running := ProxyOwner(osProxy); running {
			logger.TInfof("[dry-run] would stop xcelerate-proxy")
		} else {
			logger.TInfof("[dry-run] xcelerate-proxy not running")
		}

		return nil
	}

	if err := StopProxy(logger, osProxy); err != nil {
		return fmt.Errorf("stop xcelerate proxy: %w", err)
	}

	return nil
}

func removeXcelerateRoot(logger log.Logger, dryRun bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}

	root := paths.FromHome(home).XcelerateRoot()

	if dryRun {
		logger.TInfof("[dry-run] would remove %s", root)

		return nil
	}

	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("remove xcelerate root %s: %w", root, err)
	}

	logger.TInfof("Removed %s", root)

	return nil
}

func stripShellRC(logger log.Logger, envs map[string]string, dryRun bool) {
	if envs == nil {
		envs = map[string]string{}
	}

	if dryRun {
		logger.TInfof("[dry-run] would strip %q block from ~/.bashrc and ~/.zshrc", XcelerateShellRCBlockName)

		return
	}

	envexport.New(envs, logger).RemoveFromShellRC(XcelerateShellRCBlockName)
}
