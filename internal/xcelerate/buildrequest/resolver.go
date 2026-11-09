// Package buildrequest exposes the proxy's DerivedData-root resolution logic
// and a `build-request.json` loader shared between the proxy's per-⌘B reseat
// path and the enricher's wrapperless classification path.
package buildrequest

import (
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/deriveddata"
)

// DefaultXcodeDerivedDataRelative is Xcode's shipping DerivedData location
// under the user's home. Used when the IDE user has not overridden DD via
// Settings > Locations > Derived Data > Custom.
const DefaultXcodeDerivedDataRelative = "Library/Developer/Xcode/DerivedData"

// ResolveRoot picks the DerivedData root to scan given an ancestry-derived
// root (empty on IDE flows), falling back to Xcode's custom-location plist key
// and finally the shipping default under $HOME. Returns "" when no root can be
// determined.
//
// logger may be nil; Finder tolerates nil loggers.
func ResolveRoot(ancestryDerivedDataPath string, logger log.Logger) string {
	if ancestryDerivedDataPath != "" {
		return ancestryDerivedDataPath
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}

	finder := &deriveddata.Finder{Logger: logger}
	if custom, ok := finder.CustomRoot(home); ok {
		return custom
	}

	return filepath.Join(home, DefaultXcodeDerivedDataRelative)
}
