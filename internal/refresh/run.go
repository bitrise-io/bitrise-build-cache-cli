package refresh

import (
	"github.com/bitrise-io/go-utils/v2/log"
)

// OnBump scans the per-tool configs and reconciles them with the CLI's current
// ConfigVersion. Minor / patch bumps run their per-tool Migrator silently;
// MAJOR bumps surface a re-activate nudge through Notify. Called from the
// version-check PersistentPreRun on a CLI bump.
func OnBump(logger log.Logger, home string) {
	samples := Scan(home)
	Migrate(logger, home, samples)
	Notify(logger, samples)
}
