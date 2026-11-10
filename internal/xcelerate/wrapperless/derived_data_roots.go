package wrapperless

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// EnvDerivedDataDirs is the opt-in colon-separated list of extra DerivedData
// roots to watch. Covers CLI `-derivedDataPath` setups where the path is per
// invocation and cannot be enumerated from any persistent config.
const EnvDerivedDataDirs = "BITRISE_XCELERATE_DERIVED_DATA_DIRS"

// derivedDataRoots returns every DerivedData root the watcher must observe:
// the stock ~/Library location, Xcode's globally configured custom location
// (via `defaults read`), and any extra paths the user pins through
// EnvDerivedDataDirs. Deduplicated and filtered to absolute paths.
func derivedDataRoots(ctx context.Context, home string) []string {
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

	if custom := xcodeCustomDerivedDataLocation(ctx); custom != "" {
		add(custom)
	}

	for _, p := range strings.Split(os.Getenv(EnvDerivedDataDirs), ":") {
		add(p)
	}

	return roots
}

// xcodeCustomDerivedDataLocation returns the user's global Xcode preference
// IDECustomDerivedDataLocation, or "" when unset, relative, or unreadable.
// A relative value means per-project DerivedData which this watcher does
// not handle — skipped intentionally.
func xcodeCustomDerivedDataLocation(ctx context.Context) string {
	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx, "defaults", "read", "com.apple.dt.Xcode", "IDECustomDerivedDataLocation").Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}
