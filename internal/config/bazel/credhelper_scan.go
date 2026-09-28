package bazelconfig

import (
	"bufio"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

// PinnedHelperMatch is a repo-level bazelrc line that pins the CLI as
// --credential_helper for a Bitrise scope.
type PinnedHelperMatch struct {
	Path       string
	LineNumber int
	Line       string
}

// workspaceMarkers are the file names Bazel treats as workspace roots.
//
//nolint:gochecknoglobals
var workspaceMarkers = []string{"WORKSPACE", "WORKSPACE.bazel", "MODULE.bazel"}

// ScanForPinnedHelper walks up from startDir looking for repo-level bazelrc
// files Bazel would load ($PWD/.bazelrc, $PWD/tools/bazel.rc, workspace-root
// .bazelrc) and returns every uncommented line that pins the CLI as
// --credential_helper for a Bitrise scope.
func ScanForPinnedHelper(startDir string, osProxy utils.OsProxy) ([]PinnedHelperMatch, error) {
	if startDir == "" {
		return nil, nil
	}

	candidates := collectRcCandidates(startDir, osProxy)

	seen := make(map[string]bool, len(candidates))
	var out []PinnedHelperMatch
	for _, path := range candidates {
		if seen[path] {
			continue
		}
		seen[path] = true

		matches, err := scanFile(path, osProxy)
		if err != nil {
			return nil, err
		}
		out = append(out, matches...)
	}

	return out, nil
}

func collectRcCandidates(startDir string, osProxy utils.OsProxy) []string {
	candidates := []string{
		filepath.Join(startDir, paths.BazelrcFileName),
		filepath.Join(startDir, "tools", "bazel.rc"),
	}

	if root := findWorkspaceRoot(startDir, osProxy); root != "" && root != startDir {
		candidates = append(candidates, filepath.Join(root, paths.BazelrcFileName))
	}

	return candidates
}

// findWorkspaceRoot walks up from startDir until it finds a workspace marker
// or hits the filesystem root. Returns "" when no marker is present.
func findWorkspaceRoot(startDir string, osProxy utils.OsProxy) string {
	dir := startDir
	for {
		for _, name := range workspaceMarkers {
			if _, err := osProxy.Stat(filepath.Join(dir, name)); err == nil {
				return dir
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func scanFile(path string, osProxy utils.OsProxy) ([]PinnedHelperMatch, error) {
	content, exists, err := osProxy.ReadFileIfExists(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if !exists {
		return nil, nil
	}

	var out []PinnedHelperMatch
	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if isPinnedHelperLine(line) {
			out = append(out, PinnedHelperMatch{Path: path, LineNumber: lineNo, Line: strings.TrimSpace(line)})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}

	return out, nil
}

// isPinnedHelperLine reports whether a bazelrc line commits the Bitrise CLI as
// credential_helper. Matches either the Bitrise scope or a bare
// bitrise-build-cache binary reference. Ignores comments.
func isPinnedHelperLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}

	idx := strings.Index(trimmed, "--credential_helper=")
	if idx == -1 {
		return false
	}

	value := trimmed[idx+len("--credential_helper="):]
	if end := strings.IndexAny(value, " \t"); end != -1 {
		value = value[:end]
	}

	if strings.Contains(value, "*.services.bitrise.io") {
		return true
	}

	binary := value
	if eq := strings.Index(binary, "="); eq != -1 {
		binary = binary[eq+1:]
	}

	return filepath.Base(binary) == paths.CLIBinaryName
}
