package refresh

import (
	"io"
)

// OnBump is the integration point D1 (versioncheck) calls when it detects a
// version bump. Reads the registry and emits a refresh nudge for every
// previously-configured tool. Best-effort: returns errors for callers that
// want to surface them but never panics, never blocks.
//
// w MUST be non-nil — callers pick stderr explicitly so JSON-parsing stdout
// consumers (rn-cli wrappers) aren't disturbed.
func OnBump(w io.Writer, home, previousVersion, currentVersion string) error {
	reg, err := Load(home)
	if err != nil {
		return err
	}

	Notify(w, previousVersion, currentVersion, reg.SortedEntries())

	return nil
}
