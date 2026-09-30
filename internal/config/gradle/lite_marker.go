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
	body, err := os.ReadFile(initScriptPath(envs, home)) //nolint:gosec // path derived from home + constant
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

// initScriptPath resolves the same location activation writes to: GRADLE_USER_HOME
// when the build sets one, ~/.gradle otherwise.
func initScriptPath(envs map[string]string, home string) string {
	gradleHome := envs["GRADLE_USER_HOME"]
	if gradleHome == "" {
		gradleHome = filepath.Join(home, ".gradle")
	}

	return filepath.Join(gradleHome, "init.d", "bitrise-build-cache.init.gradle.kts")
}
