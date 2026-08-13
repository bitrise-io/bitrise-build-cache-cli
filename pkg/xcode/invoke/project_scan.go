package invoke

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// scanProjectFromCwd walks upward from cwd looking for an .xcworkspace or
// .xcodeproj. Workspace beats project at the same level; ties broken by
// lexicographic order. When repoRoot is non-empty the walk stays within
// repoRoot (cwd outside repoRoot yields ""). Returns "" when nothing is found
// — filesystem errors, no matches, and hitting the repoRoot boundary all
// collapse to that same "no hint here" outcome.
func scanProjectFromCwd(cwd, repoRoot string, osProxy utils.OsProxy, logger log.Logger) string {
	if cwd == "" {
		return ""
	}

	dir := filepath.Clean(cwd)

	var root string
	if repoRoot != "" {
		root = filepath.Clean(repoRoot)
		if !withinRoot(dir, root) {
			return ""
		}
	}

	for {
		if hit := scanDirForProject(dir, osProxy, logger); hit != "" {
			return hit
		}

		if root != "" && dir == root {
			return ""
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}

		dir = parent
	}
}

// Defensive: gitroot.Find starts at cwd so repoRoot is always an ancestor;
// this guards against callers that supply both explicitly.
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

// A ReadDir failure on any ancestor (typically permission denied) is treated
// as "no hit here" — best-effort hint gathering must not fail the wider resolve.
func scanDirForProject(dir string, osProxy utils.OsProxy, logger log.Logger) string {
	entries, err := osProxy.ReadDir(dir)
	if err != nil {
		if logger != nil {
			logger.Debugf("invoke: scanProjectFromCwd: read dir %s: %s; skipping", dir, err)
		}

		return ""
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

		return filepath.Join(dir, workspaces[0])
	case len(projects) > 0:
		sort.Strings(projects)

		return filepath.Join(dir, projects[0])
	}

	return ""
}
