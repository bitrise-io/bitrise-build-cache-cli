package ccache

import (
	"errors"
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

type DeactivateParams struct {
	DryRun bool
}

// Deactivate removes the ccache config artefact. Stopping the running storage
// helper is done from the cmd/pkg layer via pkg/ccache.StopStorageHelperAt so
// this package does not pull in internal/ccache (which itself imports us).
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

	switch err := os.Remove(configFile); {
	case err == nil:
		logger.TInfof("Removed ccache config %s", configFile)
	case os.IsNotExist(err):
		logger.Infof("ccache config already absent: %s", configFile)
	default:
		return fmt.Errorf("remove ccache config %s: %w", configFile, err)
	}

	if err := os.Remove(configDir); err != nil && !os.IsNotExist(err) {
		logger.Debugf("Leaving ccache config dir in place (%s): %s", configDir, err)
	}

	return nil
}
