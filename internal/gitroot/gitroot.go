// Package gitroot locates the enclosing git working tree by walking up
// from a start directory looking for a .git dir or (worktree) .git file.
package gitroot

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// ErrNotInGitRepo is returned when no enclosing git working tree is found.
var ErrNotInGitRepo = errors.New("not inside a git repository")

// Find walks upwards from startDir until it locates a `.git` entry
// (directory or file). Returns the absolute path of the containing repo root.
// A nil osProxy defaults to utils.DefaultOsProxy{}.
func Find(startDir string, osProxy utils.OsProxy) (string, error) {
	if osProxy == nil {
		osProxy = utils.DefaultOsProxy{}
	}

	if startDir == "" {
		return "", errors.New("startDir is empty")
	}

	abs, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}

	if _, err := osProxy.Stat(abs); err != nil {
		return "", fmt.Errorf("stat start dir: %w", err)
	}

	current := abs
	for {
		if _, err := osProxy.Stat(filepath.Join(current, ".git")); err == nil {
			return current, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", ErrNotInGitRepo
		}

		current = parent
	}
}
