package gradleconfig

import (
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
)

// SidecarMigrator implements toolconfig.Migrator for the gradle sidecar.
// Real per-version migration logic lands here when GradleConfigVersion
// grows new minor / patch fields.
type SidecarMigrator struct{}

func (SidecarMigrator) Tool() toolconfig.Tool { return toolconfig.Gradle }

func (SidecarMigrator) Migrate(home string) error {
	s, ok, err := ReadSidecar(home)
	if err != nil {
		return fmt.Errorf("read gradle sidecar: %w", err)
	}

	if !ok {
		return nil
	}

	if err := WriteSidecar(home, s); err != nil {
		return fmt.Errorf("rewrite gradle sidecar: %w", err)
	}

	return nil
}
