package refresh

import (
	"io"
	"os"
)

// Options bundles inputs for OnBump. Kept as a struct so callers can pass
// just the relevant fields without a sprawling parameter list.
type Options struct {
	// Home is the user's home directory. Use os.UserHomeDir() at the call
	// site, or t.TempDir() in tests.
	Home string
	// PreviousVersion is the CLI version that wrote the registry. Surfaced
	// in the notify text.
	PreviousVersion string
	// CurrentVersion is the running CLI version. Surfaced in the notify text.
	CurrentVersion string
	// Out is where the refresh nudge is written. Stderr in production so
	// JSON-parsing stdout consumers (rn-cli wrappers) aren't disturbed.
	// Defaults to os.Stderr when nil.
	Out io.Writer
}

// OnBump is the integration point D1 (versioncheck) calls when it detects
// a version bump. Reads the registry and emits a refresh nudge for every
// previously-configured tool. Best-effort: returns errors for callers that
// want to surface them but never panics, never blocks.
func OnBump(opts Options) error {
	if opts.Out == nil {
		opts.Out = os.Stderr
	}

	reg, err := Load(opts.Home)
	if err != nil {
		return err
	}

	Notify(opts.Out, opts.PreviousVersion, opts.CurrentVersion, reg.SortedEntries())

	return nil
}
