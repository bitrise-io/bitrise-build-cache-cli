package deriveddata

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"howett.net/plist"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/paths"
)

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
// supplied root cannot be read.
func NewestBuildRequestHash(root, workspaceHint string, now time.Time) (string, time.Time, bool) {
	if root == "" {
		return "", time.Time{}, false
	}

	roots, ok := enumerateDDRoots(root, workspaceHint)
	if !ok {
		return "", time.Time{}, false
	}

	var (
		bestHex   string
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
					bestMtime = mtime
					found = true
				}
			}
		}
	}

	if !found {
		return "", time.Time{}, false
	}

	return bestHex, bestMtime, true
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
