package wrapperless

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/deriveddata"
)

// EnvDerivedDataDirs is the opt-in colon-separated list of extra DerivedData
// roots to watch. Covers CLI `-derivedDataPath` setups where the path is per
// invocation and cannot be enumerated from any persistent config.
const EnvDerivedDataDirs = "BITRISE_XCELERATE_DERIVED_DATA_DIRS"

// derivedDataRoots returns every DerivedData root the watcher must observe:
// the stock ~/Library location, Xcode's globally configured custom location
// (parsed in-process from com.apple.dt.Xcode.plist), and any extra paths the
// user pins through EnvDerivedDataDirs. Deduplicated and filtered to absolute
// paths. A colon-separated EnvDerivedDataDirs is split with strings.Split,
// which does not understand escaping — paths containing a literal colon must
// be aliased (e.g. via a symlink) before being pinned.
func derivedDataRoots(home string) []string {
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil && h != "" {
			home = h
		}
	}

	seen := map[string]struct{}{}
	roots := []string{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || !filepath.IsAbs(p) {
			return
		}
		p = filepath.Clean(p)
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		roots = append(roots, p)
	}

	if home != "" {
		add(filepath.Join(home, "Library", "Developer", "Xcode", "DerivedData"))
	}

	// Parse the user's global Xcode preference IDECustomDerivedDataLocation
	// in-process via howett.net/plist (through deriveddata.Finder.CustomRoot)
	// rather than shelling out to `defaults read`. Avoids a 2s subprocess
	// fork and keeps every DD-root resolution path on one code path.
	if home != "" {
		finder := &deriveddata.Finder{} //nolint:exhaustruct // only CustomRoot is used
		if custom, ok := finder.CustomRoot(home); ok {
			add(custom)
		}
	}

	for _, p := range strings.Split(os.Getenv(EnvDerivedDataDirs), ":") {
		add(p)
	}

	return roots
}
