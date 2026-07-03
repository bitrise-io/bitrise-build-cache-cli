package bazelconfig

import (
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/toolconfig"
)

// SidecarMigrator implements toolconfig.Migrator for the bazel sidecar.
type SidecarMigrator struct{}

func (SidecarMigrator) Tool() toolconfig.Tool { return toolconfig.Bazel }

func (SidecarMigrator) Migrate(home string) error {
	s, ok, err := ReadSidecar(home)
	if err != nil {
		return fmt.Errorf("read bazel sidecar: %w", err)
	}

	if !ok {
		return nil
	}

	if err := WriteSidecar(home, s); err != nil {
		return fmt.Errorf("rewrite bazel sidecar: %w", err)
	}

	return nil
}
