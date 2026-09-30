package gradleconfig

import (
	"os"
	"path/filepath"
	"strings"
)

// LiteMarker is the comment a lite activation renders into the generated init
// script. It lives in the script itself, so the marker and the config it
// describes are written together and cannot disagree.
//
// A marker rather than a flag, because the plugins resolve their own token: the
// cache and analytics plugins run `auth token` directly through
// AuthTokenResolver, not through the init script's ValueSource, so nothing the
// template puts on that command line would reach them.
const LiteMarker = "// bitrise-build-cache: activated at preboot (lite)"

// IsLiteInitScript reports whether the init script in force was written by a
// lite activation. False on any error: a config we cannot prove came from
// preboot is treated as a full activation, which is the safe direction — it
// leaves the build cacheing as it does today.
func IsLiteInitScript(envs map[string]string, home string) bool {
	for _, path := range initScriptPaths(envs, home) {
		if markedLite(path) {
			return true
		}
	}

	return false
}

func markedLite(path string) bool {
	body, err := os.ReadFile(path) //nolint:gosec // path derived from home + constant
	if err != nil {
		return false
	}

	for line := range strings.SplitSeq(string(body), "\n") {
		if strings.TrimSpace(line) == LiteMarker {
			return true
		}
	}

	return false
}

// initScriptPaths lists every location the script in force could be in. Both are
// checked because preboot and the build need not agree on GRADLE_USER_HOME:
// preboot usually has none set and writes ~/.gradle, while a workflow that sets
// one later would otherwise look only there and find nothing — and a gate that
// silently does not fire is the failure this whole marker exists to avoid.
func initScriptPaths(envs map[string]string, home string) []string {
	const name = "bitrise-build-cache.init.gradle.kts"

	paths := []string{filepath.Join(home, ".gradle", "init.d", name)}
	if gradleHome := envs["GRADLE_USER_HOME"]; gradleHome != "" {
		paths = append([]string{filepath.Join(gradleHome, "init.d", name)}, paths...)
	}

	return paths
}
