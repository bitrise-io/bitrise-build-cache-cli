package refresh

import (
	"github.com/bitrise-io/go-utils/v2/log"
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

// Notify writes a multi-line refresh-needed message via logger.Warnf —
// informational-but-actionable, the user should re-run the listed commands.
// Listing each tool plus its exact rerun command keeps the user one
// copy-paste away from a fix.
//
// Returns silently when entries is empty (no tools previously configured)
// or when logger is nil.
func Notify(logger log.Logger, previousVersion, currentVersion string, entries []Entry) {
	if logger == nil || len(entries) == 0 {
		return
	}

	logger.Warnf(
		"Bitrise Build Cache CLI bumped from %s to %s. Your previously-configured build-tool configs may be out of date. Re-run the matching command(s) below to refresh:",
		previousVersion, currentVersion,
	)

	for _, e := range entries {
		if e.ConfigPath != "" {
			logger.Warnf("  • %s   # last wrote %s with CLI %s", activateCommand(e.Tool), e.ConfigPath, e.CLIVersion)
		} else {
			logger.Warnf("  • %s   # last activated with CLI %s", activateCommand(e.Tool), e.CLIVersion)
		}
	}
}
