package gradleconfig

import (
	"errors"
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/stringmerge"
)

const (
	gradleBlockStart = "# [start] generated-by-bitrise-build-cache"
	gradleBlockEnd   = "# [end] generated-by-bitrise-build-cache"
)

type DeactivateParams struct {
	GradleHome string
	Home       string
	DryRun     bool
}

func Deactivate(logger log.Logger, params DeactivateParams) error {
	var errs []error

	if err := removeGradleInitScript(logger, params.GradleHome, params.DryRun); err != nil {
		errs = append(errs, err)
	}

	if err := stripGradlePropertiesBlock(logger, params.GradleHome, params.DryRun); err != nil {
		errs = append(errs, err)
	}

	if err := removeGradleSidecar(logger, params.Home, params.DryRun); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func removeGradleInitScript(logger log.Logger, gradleHome string, dryRun bool) error {
	initScript := paths.GradleInitScript(gradleHome)

	if dryRun {
		logger.TInfof("[dry-run] would remove %s", initScript)

		return nil
	}

	if err := os.Remove(initScript); err != nil {
		if os.IsNotExist(err) {
			logger.Infof("Gradle init script already absent: %s", initScript)

			return nil
		}

		return fmt.Errorf("remove gradle init script %s: %w", initScript, err)
	}

	logger.TInfof("Removed %s", initScript)

	return nil
}

func stripGradlePropertiesBlock(logger log.Logger, gradleHome string, dryRun bool) error {
	propsPath := paths.GradlePropertiesFile(gradleHome)

	if dryRun {
		logger.TInfof("[dry-run] would strip generated block from %s", propsPath)

		return nil
	}

	current, err := os.ReadFile(propsPath) //nolint:gosec // path derived from gradleHome + constant
	if err != nil {
		if os.IsNotExist(err) {
			logger.Infof("gradle.properties already absent: %s", propsPath)

			return nil
		}

		return fmt.Errorf("read gradle.properties %s: %w", propsPath, err)
	}

	updated := stringmerge.RemoveBlock(string(current), gradleBlockStart, gradleBlockEnd)
	if updated == string(current) {
		logger.Infof("gradle.properties has no managed block: %s", propsPath)

		return nil
	}

	if err := os.WriteFile(propsPath, []byte(updated), 0o644); err != nil { //nolint:mnd,gosec
		return fmt.Errorf("write gradle.properties %s: %w", propsPath, err)
	}

	logger.TInfof("Stripped generated block from %s", propsPath)

	return nil
}

func removeGradleSidecar(logger log.Logger, home string, dryRun bool) error {
	if home == "" {
		return nil
	}

	sidecarFile := SidecarFilePath(home)
	sidecarDir := SidecarDirPath(home)

	if dryRun {
		logger.TInfof("[dry-run] would remove %s", sidecarFile)
		logger.TInfof("[dry-run] would remove %s if empty", sidecarDir)

		return nil
	}

	switch err := os.Remove(sidecarFile); {
	case err == nil:
		logger.TInfof("Removed gradle sidecar %s", sidecarFile)
	case os.IsNotExist(err):
		logger.Infof("Gradle sidecar already absent: %s", sidecarFile)
	default:
		return fmt.Errorf("remove gradle sidecar %s: %w", sidecarFile, err)
	}

	if err := os.Remove(sidecarDir); err != nil && !os.IsNotExist(err) {
		logger.Debugf("Leaving gradle sidecar dir in place (%s): %s", sidecarDir, err)
	}

	return nil
}
