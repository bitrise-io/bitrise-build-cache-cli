package invoke

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

type projectAncestorHit struct {
	projectPath string
	projectDir  string
}

// scanProjectAncestor walks upward from cwd looking for an .xcworkspace or
// .xcodeproj. Workspace beats project at the same level; ties broken by
// lexicographic order. The walk stays within repoRoot; an empty repoRoot or a
// cwd outside repoRoot yields a zero hit (invariant: no repoRoot → no
// persistence). Filesystem errors, no matches, and hitting the repoRoot
// boundary all collapse to the same "no hit here" outcome.
func scanProjectAncestor(cwd, repoRoot string, osProxy utils.OsProxy, logger log.Logger) projectAncestorHit {
	if cwd == "" || repoRoot == "" {
		return projectAncestorHit{}
	}

	dir := filepath.Clean(cwd)
	root := filepath.Clean(repoRoot)
	if !withinRoot(dir, root) {
		return projectAncestorHit{}
	}

	for {
		if hit := scanDirForProject(dir, osProxy, logger); hit != "" {
			return projectAncestorHit{projectPath: hit, projectDir: dir}
		}

		if dir == root {
			return projectAncestorHit{}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return projectAncestorHit{}
		}

		dir = parent
	}
}

// cwd outside repoRoot means the walker would leak past a workspace boundary — guard here.
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
			logger.Debugf("invoke: scanProjectAncestor: read dir %s: %s; skipping", dir, err)
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
