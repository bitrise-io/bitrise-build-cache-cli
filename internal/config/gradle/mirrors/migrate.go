package mirrors

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// MigratePrebootInitScript moves the preboot mirror init script from the default
// Gradle home into gradleHome. Preboot has no GRADLE_USER_HOME so it writes to
// ~/.gradle, which custom-GRADLE_USER_HOME builds never read. No-op when the homes
// match, no source exists, or gradleHome already has one.
func MigratePrebootInitScript(logger log.Logger, osProxy utils.OsProxy, defaultGradleHome, gradleHome string) error {
	if defaultGradleHome == gradleHome {
		return nil
	}

	src := paths.GradleMirrorsInitScript(defaultGradleHome)
	content, exists, err := osProxy.ReadFileIfExists(src)
	if err != nil {
		return fmt.Errorf("read preboot mirror init script (%s): %w", src, err)
	}
	if !exists {
		return nil
	}

	dst := paths.GradleMirrorsInitScript(gradleHome)
	if _, dstExists, err := osProxy.ReadFileIfExists(dst); err != nil {
		return fmt.Errorf("check mirror init script (%s): %w", dst, err)
	} else if dstExists {
		return nil
	}

	if err := osProxy.MkdirAll(paths.GradleInitDir(gradleHome), 0o755); err != nil {
		return fmt.Errorf("ensure Gradle init.d exists: %w", err)
	}

	if err := osProxy.WriteFile(dst, []byte(content), 0o644); err != nil { //nolint:gosec,mnd
		return fmt.Errorf("write mirror init script (%s): %w", dst, err)
	}

	if err := osProxy.Remove(src); err != nil {
		return fmt.Errorf("remove preboot mirror init script (%s): %w", src, err)
	}

	logger.Infof("(i) Relocated preboot Gradle mirrors init script to %s (honoring GRADLE_USER_HOME)", dst)

	return nil
}
