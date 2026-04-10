package dependencies

import (
	"fmt"
	"os/exec"

	"github.com/bitrise-io/go-utils/v2/log"
)

// Tool describes an external binary that needs to be available on PATH.
type Tool struct {
	Name    string
	Version string
	Install func(logger log.Logger) error
}

// IsInstalled returns true if the tool binary is found on PATH.
func (t Tool) IsInstalled() bool {
	_, err := exec.LookPath(t.Name)
	return err == nil
}

// EnsureAll checks each tool and installs it if missing.
func EnsureAll(tools []Tool, logger log.Logger) error {
	for _, t := range tools {
		if t.IsInstalled() {
			logger.Infof("✓ %s already installed", t.Name)
			continue
		}

		logger.Infof("Installing %s v%s...", t.Name, t.Version)
		if err := t.Install(logger); err != nil {
			return fmt.Errorf("install %s: %w", t.Name, err)
		}
		logger.Infof("✓ %s v%s installed", t.Name, t.Version)
	}

	return nil
}
