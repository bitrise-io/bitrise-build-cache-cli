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

// XcelerateShellRCBlockName is the marker block name shared by activate (via
// envexport.ExportToShellRC) and deactivate (via RemoveFromShellRC), so the two
// sides cannot drift.
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
//
// Home resolution honours envs["HOME"] first (so t.Setenv("HOME", ...) works
// end-to-end), else falls back to paths.Default().
func Deactivate(logger log.Logger, params DeactivateParams) error {
	var errs []error

	osProxy := utils.DefaultOsProxy{}

	home, err := resolveHome(params.Envs)
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}

	if err := stopProxyForDeactivate(logger, osProxy, params.DryRun); err != nil {
		errs = append(errs, err)
	}

	if err := removeXcelerateRoot(logger, home, params.DryRun); err != nil {
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

// resolveHome prefers envs["HOME"] (so a t.Setenv("HOME", ...) works from tests
// and CI-provided overrides propagate) and falls back to paths.Default().
func resolveHome(envs map[string]string) (string, error) {
	if v := envs["HOME"]; v != "" {
		return v, nil
	}

	p, err := paths.Default()
	if err != nil {
		return "", fmt.Errorf("default paths: %w", err)
	}

	return p.Home, nil
}

func removeXcelerateRoot(logger log.Logger, home string, dryRun bool) error {
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
