package refresh

import (
	"github.com/bitrise-io/go-utils/v2/log"
)

// OnBump scans the per-tool configs and writes a re-activate nudge for any
// tool whose persisted config schema major-version trails the CLI's current
// version. Called from the version-check PersistentPreRun on a CLI bump.
func OnBump(logger log.Logger, home string) {
	Notify(logger, Scan(home))
}
