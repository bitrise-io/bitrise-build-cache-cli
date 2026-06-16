package refresh

import (
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"
	"golang.org/x/mod/semver"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/toolconfig"
)

// CurrentConfigVersions returns the semver each tool's persisted config should
// match. Refresh nudges when the stored major lags behind these.
func CurrentConfigVersions() map[toolconfig.Tool]string {
	return map[toolconfig.Tool]string{
		toolconfig.Gradle:    toolconfig.GradleConfigVersion,
		toolconfig.Bazel:     toolconfig.BazelConfigVersion,
		toolconfig.Xcelerate: toolconfig.XcelerateConfigVersion,
		toolconfig.Ccache:    toolconfig.CcacheConfigVersion,
	}
}

func activateCommand(t toolconfig.Tool) string {
	switch t {
	case toolconfig.Gradle:
		return "bitrise-build-cache activate gradle"
	case toolconfig.Bazel:
		return "bitrise-build-cache activate bazel"
	case toolconfig.Xcelerate:
		return "bitrise-build-cache activate xcode"
	case toolconfig.Ccache:
		return "bitrise-build-cache activate c++"
	default:
		return "bitrise-build-cache activate " + string(t)
	}
}

// Notify writes a refresh-needed nudge for each sample whose stored config
// schema MAJOR version is behind the CLI's currently shipped version.
// Logger is the destination; passing nil makes Notify a no-op.
func Notify(logger log.Logger, samples []toolconfig.Sample) {
	if logger == nil || len(samples) == 0 {
		return
	}

	currents := CurrentConfigVersions()

	var stale []toolconfig.Sample
	for _, s := range samples {
		want, ok := currents[s.Tool]
		if !ok {
			continue
		}

		if needsNudge(s.ConfigVersion, want) {
			stale = append(stale, s)
		}
	}

	if len(stale) == 0 {
		return
	}

	logger.Warnf("Bitrise Build Cache config schema major bump — re-run the matching activate command(s):")
	for _, s := range stale {
		logger.Warnf("  • %s   # config %s → %s",
			activateCommand(s.Tool), displayVersion(s.ConfigVersion), currents[s.Tool])
	}
}

// needsNudge reports whether stored MAJOR < current MAJOR.
// Empty stored is treated as `v0.0.0` so the very first major bump (1.0.0 →
// 2.0.0) leaves pre-versioned configs alone but a future 2.0.0 nudges them.
func needsNudge(stored, current string) bool {
	storedV := ensureSemverPrefix(stored)
	currentV := ensureSemverPrefix(current)

	if !semver.IsValid(storedV) {
		// Older configs predate ConfigVersion; treat as the baseline (v1.0.0
		// equivalent) so the first MAJOR bump above 1 nudges them.
		storedV = "v1.0.0"
	}

	if !semver.IsValid(currentV) {
		return false
	}

	return semver.Major(storedV) != semver.Major(currentV) &&
		semver.Compare(storedV, currentV) < 0
}

func ensureSemverPrefix(v string) string {
	if strings.HasPrefix(v, "v") {
		return v
	}

	return "v" + v
}

func displayVersion(v string) string {
	if v == "" {
		return "<unversioned>"
	}

	return v
}
