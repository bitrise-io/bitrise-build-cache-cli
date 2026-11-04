package proxy

import (
	"os"
	"path/filepath"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/deriveddata"
)

// defaultXcodeDerivedDataRelative is Xcode's shipping DerivedData location
// under the user's home. Used when the IDE user has not overridden DD via
// Settings > Locations > Derived Data > Custom.
const defaultXcodeDerivedDataRelative = "Library/Developer/Xcode/DerivedData"

// NewDefaultBuildRequestResolver returns a BuildRequestResolver that discovers
// the newest <32-hex>.xcbuilddata stem written by the IDE under the user's
// DerivedData root. Resolution order per tick:
//
//  1. cs.derivedDataPath — the ancestry-derived root (populated when the peer
//     is `xcodebuild -derivedDataPath <dir>`). Empty on IDE flows.
//  2. Xcode's IDECustomDerivedDataLocation plist key, when set.
//  3. ~/Library/Developer/Xcode/DerivedData (shipping default).
//
// Logger may be nil; deriveddata's Finder tolerates nil loggers.
func NewDefaultBuildRequestResolver(logger log.Logger) BuildRequestResolver {
	finder := &deriveddata.Finder{Logger: logger}

	return func(cs *connectionSession) (string, bool) {
		root := resolveBuildRequestRoot(cs, finder)
		if root == "" {
			return "", false
		}

		hex, _, ok := deriveddata.NewestBuildRequestHash(root, "", time.Now())

		return hex, ok
	}
}

// resolveBuildRequestRoot picks the DerivedData root to scan for the given
// connectionSession. Falls back to the user's Xcode plist, then the shipping
// default under $HOME. Returns "" when no root can be determined.
func resolveBuildRequestRoot(cs *connectionSession, finder *deriveddata.Finder) string {
	if cs != nil && cs.derivedDataPath != "" {
		return cs.derivedDataPath
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}

	if custom, ok := finder.CustomRoot(home); ok {
		return custom
	}

	return filepath.Join(home, defaultXcodeDerivedDataRelative)
}
