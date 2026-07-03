package daemon

import (
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// Paths is an alias of the shared internal/paths.Paths so external callers
// don't have to import two packages when they're already in the daemon API.
type Paths = paths.Paths

// NewPathsFromHome returns Paths rooted at the supplied home dir.
func NewPathsFromHome(home string) Paths {
	return paths.FromHome(home)
}

// NewPaths returns Paths rooted at the current user's home dir.
func NewPaths() (Paths, error) {
	p, err := paths.Default()
	if err != nil {
		return p, fmt.Errorf("resolve default paths: %w", err)
	}

	return p, nil
}
