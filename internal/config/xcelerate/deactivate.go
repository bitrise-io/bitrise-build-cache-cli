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

// XcelerateShellRCBlockName is shared with activate so the two sides can't drift.
const XcelerateShellRCBlockName = "Bitrise Xcelerate"

type DeactivateParams struct {
	Envs   map[string]string
	DryRun bool
}

// Deactivate stops the proxy, removes ~/.bitrise-xcelerate/, and strips the
// shell RC block. Log/queue state under ~/.local/state/xcelerate/* is kept for
// debugging; the auth credential store is owned by `auth logout`.
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
