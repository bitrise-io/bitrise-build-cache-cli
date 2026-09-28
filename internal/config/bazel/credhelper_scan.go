package bazelconfig

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
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

// GitTrackedFn reports whether the given absolute path is tracked by git.
// A nil GitTrackedFn tells ScanForPinnedHelper to fall back to DefaultGitTrackedFn.
type GitTrackedFn func(path string) bool

// workspaceMarkers are the file names Bazel treats as workspace roots.
//
//nolint:gochecknoglobals
var workspaceMarkers = []string{"WORKSPACE", "WORKSPACE.bazel", "MODULE.bazel"}

// ScanForPinnedHelper walks up from startDir looking for repo-level bazelrc
// files Bazel would load ($PWD/.bazelrc, $PWD/tools/bazel.rc, workspace-root
// .bazelrc) and returns every uncommented line that pins the CLI as
// --credential_helper for a Bitrise scope.
//
// Only files git considers tracked are scanned — activation itself writes a
// per-user ~/.bazelrc that git would ignore, and we do not want to warn about
// our own output. When git isn't available (missing binary, path outside a
// repo), the file is treated as tracked so a real committed pin still surfaces.
func ScanForPinnedHelper(ctx context.Context, startDir string, osProxy utils.OsProxy, isTracked GitTrackedFn) ([]PinnedHelperMatch, error) {
	if startDir == "" {
		return nil, nil
	}
	if isTracked == nil {
		isTracked = DefaultGitTrackedFn(ctx)
	}

	candidates := collectRcCandidates(startDir, osProxy)

	seen := make(map[string]bool, len(candidates))
	var out []PinnedHelperMatch
	for _, path := range candidates {
		if seen[path] {
			continue
		}
		seen[path] = true

		if !isTracked(path) {
			continue
		}

		matches, err := scanFile(path, osProxy)
		if err != nil {
			return nil, err
		}
		out = append(out, matches...)
	}

	return out, nil
}

// DefaultGitTrackedFn returns a GitTrackedFn backed by `git ls-files
// --error-unmatch`. Any error (git missing, path outside a repo, path
// untracked) is treated as "not tracked" — except when git itself is missing,
// in which case the path is treated as tracked so a real committed pin on a
// git-less machine (rare, but possible in build sandboxes) still surfaces.
func DefaultGitTrackedFn(ctx context.Context) GitTrackedFn {
	gitPath, gitAvailable := lookGit()

	return func(path string) bool {
		if !gitAvailable {
			return true
		}

		dir := filepath.Dir(path)
		base := filepath.Base(path)

		cmd := exec.CommandContext(ctx, gitPath, "ls-files", "--error-unmatch", "--", base)
		cmd.Dir = dir

		return cmd.Run() == nil
	}
}

//nolint:gochecknoglobals // resolved once at process start; injected in tests
var lookGit = func() (string, bool) {
	p, err := exec.LookPath("git")
	if err != nil {
		return "", false
	}

	return p, true
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
// bitrise-build-cache binary reference. Handles both `--credential_helper=X`
// and space-separated `--credential_helper X`. Ignores comments.
func isPinnedHelperLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}

	value, ok := extractCredHelperValue(trimmed)
	if !ok {
		return false
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

// extractCredHelperValue pulls the value (SCOPE=BIN or BIN) out of a
// --credential_helper flag, whether it is written with `=` or a space
// separator.
func extractCredHelperValue(line string) (string, bool) {
	const flag = "--credential_helper"

	idx := strings.Index(line, flag)
	if idx == -1 {
		return "", false
	}

	rest := line[idx+len(flag):]
	if rest == "" {
		return "", false
	}

	switch rest[0] {
	case '=':
		rest = rest[1:]
	case ' ', '\t':
		rest = strings.TrimLeft(rest, " \t")
	default:
		return "", false
	}

	if end := strings.IndexAny(rest, " \t"); end != -1 {
		rest = rest[:end]
	}
	if rest == "" {
		return "", false
	}

	return rest, true
}
