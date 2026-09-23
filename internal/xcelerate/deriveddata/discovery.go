// Package deriveddata discovers the most recently-run Xcode invocation for a
// given command by scanning DerivedData LogStoreManifest.plist entries.
package deriveddata

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
	"howett.net/plist"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

// LatestBuild holds the fields discoverable from a recent DerivedData manifest.
// Exactly one of Workspace/Project is set when the surrounding info.plist is
// readable; both may be empty when it isn't. Destination is never discoverable
// from the manifest — callers must prompt for it.
type LatestBuild struct {
	Workspace     string
	Project       string
	Scheme        string
	Configuration string
}

// ErrNoRecentBuild signals no matching manifest entry was found.
var ErrNoRecentBuild = errors.New("no recent build found in DerivedData")

// Finder scans DerivedData for the most recent manifest entry matching a command.
// Nil Logger falls back to a discard logger.
type Finder struct {
	Logger log.Logger

	// HomeDir overrides os.UserHomeDir for tests. Empty falls back to the real home.
	HomeDir string

	// ProjectPathHint filters DD roots to those whose info.plist WorkspacePath
	// matches this project. Empty disables the filter (all entries considered).
	ProjectPathHint string
}

// LatestForCommand returns the most recent build matching command. Returns
// ErrNoRecentBuild if no manifest entry matched.
//
// Picks the entry with the latest Stop timestamp regardless of Status; a failed
// most-recent build wins over an earlier success.
func (f *Finder) LatestForCommand(command enrichment.Command) (LatestBuild, error) {
	logger := f.logger()

	home, err := f.homeDir()
	if err != nil {
		return LatestBuild{}, fmt.Errorf("resolve home dir: %w", err)
	}

	globs := []string{
		enrichment.DefaultDerivedDataGlob,
		paths.XcodeManagedDerivedDataManifestGlobRelative,
	}

	var (
		best      enrichment.ManifestEntry
		bestPath  string
		bestFound bool
	)

	enrichment.WalkManifests(home, globs, logger, func(path string, entries []enrichment.ManifestEntry) {
		if f.ProjectPathHint != "" && !f.matchesHint(path) {
			return
		}

		for _, entry := range entries {
			if entry.Command() != command || entry.Stop.IsZero() {
				continue
			}

			if !bestFound || entry.Stop.After(best.Stop) {
				best = entry
				bestPath = path
				bestFound = true
			}
		}
	})

	if !bestFound {
		return LatestBuild{}, ErrNoRecentBuild
	}

	result := LatestBuild{
		Scheme:        best.SchemeName,
		Configuration: extractConfiguration(best.Signature),
	}

	// <DD-root>/Logs/<Build|Test>/LogStoreManifest.plist -> <DD-root>
	ddRoot := filepath.Dir(filepath.Dir(filepath.Dir(bestPath)))
	if ws, proj := f.readWorkspaceInfo(ddRoot); ws != "" || proj != "" {
		result.Workspace = ws
		result.Project = proj
	}

	return result, nil
}

// Xcode writes signatures like "Cleaning project X with scheme Y and configuration Debug".
func extractConfiguration(signature string) string {
	const key = "configuration "

	idx := strings.Index(signature, key)
	if idx < 0 {
		return ""
	}

	rest := signature[idx+len(key):]

	end := strings.Index(rest, " ")
	if end < 0 {
		return strings.TrimSpace(rest)
	}

	return strings.TrimSpace(rest[:end])
}

// Returns (workspace, project); exactly one is set based on WorkspacePath's extension.
func (f *Finder) readWorkspaceInfo(ddRoot string) (string, string) {
	name := filepath.Base(f.readWorkspacePathRaw(ddRoot))
	switch {
	case strings.HasSuffix(name, ".xcworkspace"):
		return name, ""
	case strings.HasSuffix(name, ".xcodeproj"):
		return "", name
	default:
		return "", ""
	}
}

func (f *Finder) readWorkspacePathRaw(ddRoot string) string {
	logger := f.logger()

	infoPath := filepath.Join(ddRoot, "info.plist")

	file, err := os.Open(infoPath)
	if err != nil {
		logger.Debugf("deriveddata: read %q failed: %s", infoPath, err)

		return ""
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		logger.Debugf("deriveddata: read %q failed: %s", infoPath, err)

		return ""
	}

	var info struct {
		WorkspacePath string `plist:"WorkspacePath"`
	}
	if _, err := plist.Unmarshal(data, &info); err != nil {
		logger.Debugf("deriveddata: decode %q failed: %s", infoPath, err)

		return ""
	}

	return info.WorkspacePath
}

// An unreadable info.plist (or one with an empty WorkspacePath) is treated as
// a mismatch under an active filter.
func (f *Finder) matchesHint(manifestPath string) bool {
	ddRoot := filepath.Dir(filepath.Dir(filepath.Dir(manifestPath)))

	raw := f.readWorkspacePathRaw(ddRoot)
	if raw == "" {
		f.logger().Debugf("deriveddata: skipping %s: no readable info.plist under project filter", ddRoot)

		return false
	}

	return matchesProject(raw, f.ProjectPathHint)
}

// Resolves symlinks when possible; falls back to filepath.Clean on failure.
// Compare is case-insensitive to match APFS default semantics.
func matchesProject(candidate, hint string) bool {
	if candidate == "" && hint == "" {
		return true
	}

	return strings.EqualFold(normalizeProjectPath(candidate), normalizeProjectPath(hint))
}

func normalizeProjectPath(p string) string {
	if p == "" {
		return ""
	}

	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(resolved)
	}

	return filepath.Clean(p)
}

//nolint:gochecknoglobals // shared discard logger avoids per-call allocation
var noopLogger = log.NewLogger(log.WithOutput(io.Discard))

func (f *Finder) logger() log.Logger {
	if f.Logger == nil {
		return noopLogger
	}

	return f.Logger
}

func (f *Finder) homeDir() (string, error) {
	if f.HomeDir != "" {
		return f.HomeDir, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}

	return home, nil
}
