package doctor

import (
	"errors"
	"fmt"

	machineconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/machine"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// ProjectScopeFixer drops a marker at the current build directory so the
// opt-in gate stops skipping cache for it. Prompt is injected by the doctor
// command; nil leaves the fixer pointing at `project enable`.
type ProjectScopeFixer struct {
	Dir     string
	OsProxy utils.OsProxy
	Prompt  func(dir string) (bool, error)
}

func (f ProjectScopeFixer) NeedsTerminal() bool { return true }

func (f ProjectScopeFixer) Fix() (string, error) {
	if f.Prompt == nil {
		return "", errors.New("no confirm prompt wired: run `bitrise-build-cache project enable` to opt this directory in")
	}

	confirmed, err := f.Prompt(f.Dir)
	if err != nil {
		return "", fmt.Errorf("project-scope prompt: %w", err)
	}
	if !confirmed {
		return "skipped (user declined)", nil
	}

	wrote, ancestor, err := machineconfig.WriteMarkerIfMissing(f.Dir, f.OsProxy)
	if err != nil {
		return "", fmt.Errorf("write marker: %w", err)
	}
	if ancestor != "" {
		return "already covered by " + ancestor, nil
	}

	return "wrote .bitrise-build-cache.json at " + wrote, nil
}
