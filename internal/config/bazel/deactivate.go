package bazelconfig

import (
	"errors"
	"fmt"
	"os"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/stringmerge"
)

// DeactivateParams controls the bazel deactivate flow.
type DeactivateParams struct {
	// BazelrcPath is the target ~/.bazelrc.
	BazelrcPath string
	// Home is the user's home dir, used to locate the sidecar under ~/.bitrise/cache/bazel.
	Home string
	// DryRun logs intended actions instead of performing them.
	DryRun bool
}

// Deactivate undoes what Activate wrote for Bazel:
//   - strips the marker block from ~/.bazelrc
//   - deletes ~/.bitrise/cache/bazel/config.json and its containing dir when empty
func Deactivate(logger log.Logger, params DeactivateParams) error {
	var errs []error

	if err := stripBazelrcBlock(logger, params.BazelrcPath, params.DryRun); err != nil {
		errs = append(errs, err)
	}

	if err := removeBazelSidecar(logger, params.Home, params.DryRun); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func stripBazelrcBlock(logger log.Logger, bazelrcPath string, dryRun bool) error {
	if dryRun {
		logger.TInfof("[dry-run] would strip generated block from %s", bazelrcPath)

		return nil
	}

	current, err := os.ReadFile(bazelrcPath) //nolint:gosec // caller-supplied path
	if err != nil {
		if os.IsNotExist(err) {
			logger.Infof(".bazelrc already absent: %s", bazelrcPath)

			return nil
		}

		return fmt.Errorf("read bazelrc %s: %w", bazelrcPath, err)
	}

	updated := stringmerge.RemoveBlock(string(current), bazelBlockStart, bazelBlockEnd)
	if updated == string(current) {
		logger.Infof(".bazelrc has no managed block: %s", bazelrcPath)

		return nil
	}

	if err := os.WriteFile(bazelrcPath, []byte(updated), 0o644); err != nil { //nolint:mnd,gosec
		return fmt.Errorf("write bazelrc %s: %w", bazelrcPath, err)
	}

	logger.TInfof("Stripped generated block from %s", bazelrcPath)

	return nil
}

func removeBazelSidecar(logger log.Logger, home string, dryRun bool) error {
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
		logger.TInfof("Removed bazel sidecar %s", sidecarFile)
	case os.IsNotExist(err):
		logger.Infof("Bazel sidecar already absent: %s", sidecarFile)
	default:
		return fmt.Errorf("remove bazel sidecar %s: %w", sidecarFile, err)
	}

	if err := os.Remove(sidecarDir); err != nil && !os.IsNotExist(err) {
		logger.Debugf("Leaving bazel sidecar dir in place (%s): %s", sidecarDir, err)
	}

	return nil
}
