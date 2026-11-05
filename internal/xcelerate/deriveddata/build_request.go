package deriveddata

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"howett.net/plist"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

// BuildRequest captures the classification fields the enrichment layer reads
// from Xcode's build-request.json inside `<hex>.xcbuilddata`. All fields are
// optional on disk; readers must tolerate zero values.
type BuildRequest struct {
	// Command is buildCommand.command — the primary action label
	// (e.g. "prepareForIndexing", "build", "archive", "clean").
	Command string
	// Action is parameters.action — the fallback label used when Command is
	// empty (e.g. "build", "indexbuild", "archive", "clean", "test").
	Action string
	// Configuration is parameters.configurationName (e.g. "Debug", "Release"),
	// empty when absent.
	Configuration string
	// Container is the basename of containerPath with the `.xcodeproj` or
	// `.xcworkspace` suffix stripped (e.g. "IceCubesApp"), empty when absent.
	Container string
}

// IsEmpty reports whether every classification field is the zero value.
func (b BuildRequest) IsEmpty() bool {
	return b.Command == "" && b.Action == "" && b.Configuration == "" && b.Container == ""
}

// buildRequestFile mirrors the subset of Xcode's build-request.json the
// enrichment layer consumes. Unknown fields are ignored by encoding/json so
// future schema additions degrade gracefully.
type buildRequestFile struct {
	BuildCommand struct {
		Command string `json:"command"`
	} `json:"buildCommand"`
	Parameters struct {
		Action            string `json:"action"`
		ConfigurationName string `json:"configurationName"`
	} `json:"parameters"`
	ContainerPath string `json:"containerPath"`
}

// ReadBuildRequest opens `<dir>/build-request.json` and returns the typed
// classification fields alongside an ok flag. Any error — missing file,
// malformed JSON, or an unreadable path — returns a zero BuildRequest + false,
// since classification is best-effort.
func ReadBuildRequest(dir string) (BuildRequest, bool) {
	if dir == "" {
		return BuildRequest{}, false
	}

	path := filepath.Join(dir, "build-request.json")

	data, err := os.ReadFile(path)
	if err != nil {
		return BuildRequest{}, false
	}

	var raw buildRequestFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return BuildRequest{}, false
	}

	return BuildRequest{
		Command:       raw.BuildCommand.Command,
		Action:        raw.Parameters.Action,
		Configuration: raw.Parameters.ConfigurationName,
		Container:     containerBasename(raw.ContainerPath),
	}, true
}

// containerBasename strips the `.xcodeproj` / `.xcworkspace` suffix off the
// containerPath basename so the enrichment layer renders the bare project
// name.
func containerBasename(containerPath string) string {
	if containerPath == "" {
		return ""
	}

	name := filepath.Base(containerPath)
	name = strings.TrimSuffix(name, ".xcodeproj")
	name = strings.TrimSuffix(name, ".xcworkspace")

	return name
}

// xcodeDefaultsPlistRelative is the per-user Xcode defaults plist that holds
// IDECustomDerivedDataLocation when the user has moved DerivedData out of the
// standard ~/Library/Developer/Xcode/DerivedData.
const xcodeDefaultsPlistRelative = "Library/Preferences/com.apple.dt.Xcode.plist"

// buildRequestHashRE matches the 32-hex basename stem Xcode writes under
// XCBuildData for each ⌘B. Guarded by the regex so a stray dir like
// "Script-ABCD.xcbuilddata" cannot be mistaken for a build-request hash.
//
//nolint:gochecknoglobals // compiled once, read-only
var buildRequestHashRE = regexp.MustCompile(`^[0-9a-f]{32}$`)

// CustomRoot returns the user's custom DerivedData root as configured via
// Xcode > Settings > Locations > Derived Data > Custom. Reads the
// IDECustomDerivedDataLocation key from com.apple.dt.Xcode.plist under the
// supplied home. Returns ("", false) when the plist cannot be read, the key is
// absent, or the value is empty — callers fall back to the standard DD root.
func (f *Finder) CustomRoot(home string) (string, bool) {
	plistPath := filepath.Join(home, xcodeDefaultsPlistRelative)

	file, err := os.Open(plistPath)
	if err != nil {
		f.logger().Debugf("deriveddata: read %q failed: %s", plistPath, err)

		return "", false
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		f.logger().Debugf("deriveddata: read %q failed: %s", plistPath, err)

		return "", false
	}

	var defs struct {
		CustomDerivedDataLocation string `plist:"IDECustomDerivedDataLocation"`
	}
	if _, err := plist.Unmarshal(data, &defs); err != nil {
		f.logger().Debugf("deriveddata: decode %q failed: %s", plistPath, err)

		return "", false
	}

	trimmed := strings.TrimSpace(defs.CustomDerivedDataLocation)
	if trimmed == "" {
		return "", false
	}

	return trimmed, true
}

// NewestBuildRequestHash scans root for the newest-mtime `<32-hex>.xcbuilddata`
// entry across every workspace-sha DD subdir, under both the user-build and
// index-store XCBuildData globs. workspaceHint, when non-empty, restricts the
// scan to DD subdirs whose name begins with it (Xcode's "<ws>-<hash>" prefix);
// empty matches every subdir, which is the MVP behaviour the proxy uses when
// it has no workspace scope yet.
//
// Returns (hex, mtime, ok). ok=false means no matching entry exists or the
// supplied root cannot be read. Prefer NewestBuildRequestEntry when the caller
// also needs the `.xcbuilddata` directory path (e.g. to read build-request.json).
func NewestBuildRequestHash(root, workspaceHint string, now time.Time) (string, time.Time, bool) {
	hex, _, mtime, ok := NewestBuildRequestEntry(root, workspaceHint, now)

	return hex, mtime, ok
}

// NewestBuildRequestEntry is the richer sibling of NewestBuildRequestHash: it
// returns the winning entry's `.xcbuilddata` directory path alongside the
// hex stem, so a caller can ReadBuildRequest(dir) without re-globbing. Empty
// path + ok=false on every miss path.
func NewestBuildRequestEntry(root, workspaceHint string, now time.Time) (string, string, time.Time, bool) {
	if root == "" {
		return "", "", time.Time{}, false
	}

	roots, ok := enumerateDDRoots(root, workspaceHint)
	if !ok {
		return "", "", time.Time{}, false
	}

	var (
		bestHex   string
		bestDir   string
		bestMtime time.Time
		found     bool
	)

	for _, dd := range roots {
		for _, rel := range []string{
			paths.XcodeBuildRequestHashGlobRelative,
			paths.XcodeIndexBuildRequestHashGlobRelative,
		} {
			matches, _ := filepath.Glob(filepath.Join(dd, rel))
			for _, m := range matches {
				stem := strings.TrimSuffix(filepath.Base(m), ".xcbuilddata")
				if !buildRequestHashRE.MatchString(stem) {
					continue
				}

				st, err := os.Stat(m)
				if err != nil {
					continue
				}

				mtime := st.ModTime()
				// Future-dated mtimes (clock skew, imaged disks) collapse to now so a
				// bogus timestamp cannot win over a legitimate recent build.
				if mtime.After(now) {
					mtime = now
				}

				if !found || mtime.After(bestMtime) {
					bestHex = stem
					bestDir = m
					bestMtime = mtime
					found = true
				}
			}
		}
	}

	if !found {
		return "", "", time.Time{}, false
	}

	return bestHex, bestDir, bestMtime, true
}

// enumerateDDRoots lists workspace-sha dirs directly under root. workspaceHint
// restricts the result set to names starting with that prefix.
func enumerateDDRoots(root, workspaceHint string) ([]string, bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, false
	}

	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		if workspaceHint != "" && !strings.HasPrefix(e.Name(), workspaceHint) {
			continue
		}

		out = append(out, filepath.Join(root, e.Name()))
	}

	return out, true
}
