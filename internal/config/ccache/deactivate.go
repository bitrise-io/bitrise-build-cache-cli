package ccache

import (
	"errors"
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// DeactivateParams controls the ccache config cleanup half of deactivate.
// Stopping the running storage helper lives in pkg/ccache to keep this
// package free of the internal/ccache IPC dep.
type DeactivateParams struct {
	// DryRun logs intended actions instead of performing them.
	DryRun bool
}

// Deactivate removes the ccache config artefact:
//   - ~/.bitrise/cache/ccache/config.json
//   - the containing dir when it's empty
//
// Log files under ~/.local/state/ccache/logs are intentionally preserved.
// Stopping the running storage helper is done from the cmd/pkg layer via
// pkg/ccache.StopStorageHelperAt so this package does not pull in
// internal/ccache (which itself imports us).
func Deactivate(logger log.Logger, params DeactivateParams) error {
	var errs []error

	osProxy := utils.DefaultOsProxy{}

	if err := removeCcacheConfig(logger, osProxy, params.DryRun); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func removeCcacheConfig(logger log.Logger, osProxy utils.OsProxy, dryRun bool) error {
	configFile := ConfigFile(osProxy)
	configDir := DirPath(osProxy)

	if dryRun {
		logger.TInfof("[dry-run] would remove %s", configFile)
		logger.TInfof("[dry-run] would remove %s if empty", configDir)

		return nil
	}

	if err := os.Remove(configFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove ccache config %s: %w", configFile, err)
	}

	if err := os.Remove(configDir); err != nil && !os.IsNotExist(err) {
		logger.Debugf("Leaving ccache config dir in place (%s): %s", configDir, err)
	}

	logger.TInfof("Removed ccache config %s", configFile)

	return nil
}
