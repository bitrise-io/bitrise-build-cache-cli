package reactnative

import (
	"strings"
)

// EASWorkingDirEnv is the env var EAS Build reads to decide where to stage the
// local build working directory. EAS picks a random tmp dir per invocation
// otherwise, which defeats any source-path-sensitive cache (Gradle, Xcode,
// ccache all hash absolute paths).
const EASWorkingDirEnv = "EAS_LOCAL_BUILD_WORKINGDIR"

// IsEASBuildInvocation reports whether the wrapped command is `eas build ...`,
// either invoked directly or through a common JS package-manager runner
// (npx / pnpm / yarn / bunx / bun). The detection is intentionally conservative
// — only the exact `build` subcommand qualifies, since EAS_LOCAL_BUILD_WORKINGDIR
// is only consulted by `eas build --local`.
func IsEASBuildInvocation(name string, cmdArgs []string) bool {
	if name == "eas" && len(cmdArgs) > 0 && cmdArgs[0] == "build" {
		return true
	}

	if len(cmdArgs) >= 2 && cmdArgs[0] == "eas" && cmdArgs[1] == "build" {
		switch name {
		case "npx", "pnpm", "yarn", "bunx", "bun":
			return true
		}
	}

	return false
}

// DefaultEASWorkingDir picks a stable working dir for EAS_LOCAL_BUILD_WORKINGDIR.
// We anchor it under $HOME so the path automatically follows whatever user
// the current stack runs as (/Users/vagrant today, anything tomorrow). EAS
// creates the directory on first use — it only needs to be stable, not
// pre-existing.
//
// Returns "" when HOME is not present in envs; callers must treat that as a
// signal to skip the injection rather than fall back to a hardcoded path.
func DefaultEASWorkingDir(envs map[string]string) string {
	home := envs["HOME"]
	if home == "" {
		return ""
	}

	return home + "/build"
}

// environToMap parses a Go-style environ slice into a map.
func environToMap(environ []string) map[string]string {
	out := make(map[string]string, len(environ))
	for _, entry := range environ {
		if idx := strings.IndexByte(entry, '='); idx > 0 {
			out[entry[:idx]] = entry[idx+1:]
		}
	}

	return out
}

// mapToEnviron renders an env map back into a Go-style environ slice for
// passing to os/exec. Order is not preserved (Go maps are unordered) — that's
// fine for child-process semantics, but callers that need a specific order
// must sort the result themselves.
func mapToEnviron(envs map[string]string) []string {
	out := make([]string, 0, len(envs))
	for k, v := range envs {
		out = append(out, k+"="+v)
	}

	return out
}
