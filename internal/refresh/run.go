package refresh

import (
	"github.com/bitrise-io/go-utils/v2/log"
)

// OnBump is the integration point D1 (versioncheck) calls when it detects a
// version bump. Reads the registry and emits a refresh nudge for every
// previously-configured tool. Best-effort: returns errors for callers that
// want to surface them but never panics, never blocks.
//
// logger MUST be non-nil — callers pick a logger backed by stderr in
// production so JSON-parsing stdout consumers (rn-cli wrappers) aren't
// disturbed.
func OnBump(logger log.Logger, home, previousVersion, currentVersion string) error {
	reg, err := Load(home)
	if err != nil {
		return err
	}

	Notify(logger, previousVersion, currentVersion, reg.SortedEntries())

	return nil
}
