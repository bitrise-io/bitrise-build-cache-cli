package refresh

import (
	"github.com/bitrise-io/go-utils/v2/log"
	"golang.org/x/mod/semver"

	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/bazel"
	ccacheconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/ccache"
	gradleconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/gradle"
	xcelerateconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/xcelerate"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
)

// DefaultMigrators returns the per-tool migrators refresh dispatches on
// non-major ConfigVersion bumps. Tests pass their own slice via MigrateWith.
func DefaultMigrators(logger log.Logger) []toolconfig.Migrator {
	return []toolconfig.Migrator{
		gradleconfig.SidecarMigrator{},
		bazelconfig.SidecarMigrator{},
		xcelerateconfig.ConfigMigrator{Logger: logger},
		ccacheconfig.ConfigMigrator{Logger: logger},
	}
}

// Migrate runs each registered migrator for tools whose stored ConfigVersion
// is behind the current within the same MAJOR (minor / patch bumps). Major
// bumps stay in Notify's hands — Migrate explicitly skips them.
func Migrate(logger log.Logger, home string, samples []toolconfig.Sample) {
	MigrateWith(logger, home, samples, DefaultMigrators(logger))
}

// MigrateWith is Migrate with an injectable migrator slice for tests.
func MigrateWith(logger log.Logger, home string, samples []toolconfig.Sample, migrators []toolconfig.Migrator) {
	if logger == nil {
		return
	}

	currents := CurrentConfigVersions()
	byTool := make(map[toolconfig.Tool]toolconfig.Migrator, len(migrators))

	for _, m := range migrators {
		byTool[m.Tool()] = m
	}

	for _, s := range samples {
		want, ok := currents[s.Tool]
		if !ok {
			continue
		}

		if !needsMigrate(s.ConfigVersion, want) {
			continue
		}

		m, ok := byTool[s.Tool]
		if !ok {
			continue
		}

		if err := m.Migrate(home); err != nil {
			logger.Debugf("Auto-migrate %s config failed: %v", s.Tool, err)

			continue
		}

		logger.Debugf("Auto-migrated %s config to %s", s.Tool, want)
	}
}

// needsMigrate reports whether stored is behind current within the same MAJOR.
// MAJOR bumps are user-facing (Notify); same-version or future-version returns false.
func needsMigrate(stored, current string) bool {
	storedV := ensureSemverPrefix(stored)
	currentV := ensureSemverPrefix(current)

	if !semver.IsValid(storedV) {
		storedV = "v1.0.0"
	}

	if !semver.IsValid(currentV) {
		return false
	}

	return semver.Major(storedV) == semver.Major(currentV) &&
		semver.Compare(storedV, currentV) < 0
}
