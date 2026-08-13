package invoke

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// scanProjectFromCwd walks upward from cwd looking for an .xcworkspace or
// .xcodeproj. Workspace beats project at the same level; ties broken by
// lexicographic order. When repoRoot is non-empty the walk stays within
// repoRoot (cwd outside repoRoot yields ""). Returns "" when nothing is found.
func scanProjectFromCwd(cwd, repoRoot string, osProxy utils.OsProxy) (string, error) {
	if cwd == "" {
		return "", nil
	}

	dir := filepath.Clean(cwd)

	var root string
	if repoRoot != "" {
		root = filepath.Clean(repoRoot)
		if !withinRoot(dir, root) {
			return "", nil
		}
	}

	for {
		hit, err := scanDirForProject(dir, osProxy)
		if err != nil {
			return "", err
		}

		if hit != "" {
			return hit, nil
		}

		if root != "" && dir == root {
			return "", nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}

		dir = parent
	}
}

func withinRoot(dir, root string) bool {
	if dir == root {
		return true
	}

	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return false
	}

	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func scanDirForProject(dir string, osProxy utils.OsProxy) (string, error) {
	entries, err := osProxy.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read dir %s: %w", dir, err)
	}

	var workspaces, projects []string
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasSuffix(name, ".xcworkspace"):
			workspaces = append(workspaces, name)
		case strings.HasSuffix(name, ".xcodeproj"):
			projects = append(projects, name)
		}
	}

	switch {
	case len(workspaces) > 0:
		sort.Strings(workspaces)

		return filepath.Join(dir, workspaces[0]), nil
	case len(projects) > 0:
		sort.Strings(projects)

		return filepath.Join(dir, projects[0]), nil
	}

	return "", nil
}
