package proxy

import (
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/buildrequest"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/deriveddata"
)

// NewDefaultBuildRequestResolver returns a BuildRequestResolver that discovers
// the newest <32-hex>.xcbuilddata stem written by the IDE under the user's
// DerivedData root. Resolution of the root is delegated to
// internal/xcelerate/buildrequest so the enricher side can share it.
//
// Logger may be nil; deriveddata's Finder tolerates nil loggers.
func NewDefaultBuildRequestResolver(logger log.Logger) BuildRequestResolver {
	return func(cs *connectionSession) (string, bool) {
		var ancestryDD string
		if cs != nil {
			ancestryDD = cs.derivedDataPath
		}

		root := buildrequest.ResolveRoot(ancestryDD, logger)
		if root == "" {
			return "", false
		}

		hex, _, ok := deriveddata.NewestBuildRequestHash(root, "", time.Now())

		return hex, ok
	}
}
