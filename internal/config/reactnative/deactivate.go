package reactnative

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

// Deactivate removes only the RN marker file. Fan-out to gradle/xcode/ccache
// lives at the cmd layer so wiring stays visible.
func Deactivate(logger log.Logger, params DeactivateParams) error {
	var errs []error

	osProxy := utils.DefaultOsProxy{}
	configFile := PathFor(osProxy, ConfigFileName)
	configDir := DirPath(osProxy)

	if params.DryRun {
		logger.TInfof("[dry-run] would remove %s", configFile)
		logger.TInfof("[dry-run] would remove %s if empty", configDir)

		return nil
	}

	switch err := os.Remove(configFile); {
	case err == nil:
		logger.TInfof("Removed react-native marker %s", configFile)
	case os.IsNotExist(err):
		logger.Infof("React Native marker already absent: %s", configFile)
	default:
		errs = append(errs, fmt.Errorf("remove react-native marker %s: %w", configFile, err))
	}

	if err := os.Remove(configDir); err != nil && !os.IsNotExist(err) {
		logger.Debugf("Leaving react-native config dir in place (%s): %s", configDir, err)
	}

	return errors.Join(errs...)
}
