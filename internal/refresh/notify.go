package refresh

import (
	"fmt"
	"io"
)

// activateCommand returns the exact CLI command a user should run to refresh
// the supplied tool's config. Kept in one place so the nudge text and any
// future help / docs stay consistent.
func activateCommand(tool string) string {
	switch tool {
	case ToolGradle:
		return "bitrise-build-cache activate gradle"
	case ToolBazel:
		return "bitrise-build-cache activate bazel"
	case ToolXcelerate:
		return "bitrise-build-cache activate xcode"
	case ToolCcache:
		return "bitrise-build-cache activate c++"
	default:
		return "bitrise-build-cache activate " + tool
	}
}

// Notify writes a multi-line refresh-needed message to w. Listing each tool
// plus its exact rerun command keeps the user one copy-paste away from a fix.
//
// Returns silently when entries is empty — no tools previously configured
// means nothing to nudge about.
func Notify(w io.Writer, previousVersion, currentVersion string, entries []Entry) {
	if len(entries) == 0 {
		return
	}

	_, _ = fmt.Fprintf(w,
		"Bitrise Build Cache CLI bumped from %s to %s. Your previously-configured build-tool configs may be out of date. Re-run the matching command(s) below to refresh:\n",
		previousVersion, currentVersion,
	)

	for _, e := range entries {
		if e.ConfigPath != "" {
			_, _ = fmt.Fprintf(w, "  • %s   # last wrote %s with CLI %s\n", activateCommand(e.Tool), e.ConfigPath, e.CLIVersion)
		} else {
			_, _ = fmt.Fprintf(w, "  • %s   # last activated with CLI %s\n", activateCommand(e.Tool), e.CLIVersion)
		}
	}
}
