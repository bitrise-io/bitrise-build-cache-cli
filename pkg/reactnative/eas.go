package reactnative

import (
	"strings"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
)

// EASWorkingDirEnv is the env var EAS Build reads to decide where to stage the
// local build working directory. EAS picks a random tmp dir per invocation
// otherwise, which defeats any source-path-sensitive cache (Gradle, Xcode,
// ccache all hash absolute paths).
const EASWorkingDirEnv = "EAS_LOCAL_BUILD_WORKINGDIR"

// bitriseDefaultEASWorkingDir matches the convention documented in
// ACI-4952 / the Bitrise example workflow. The vagrant user's HOME is
// /Users/vagrant on Bitrise macOS stacks.
const bitriseDefaultEASWorkingDir = "/Users/vagrant/build"

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
// On Bitrise CI we keep the documented /Users/vagrant/build path; on every
// other CI and on local machines we fall back to $HOME/build (EAS creates the
// directory on first use — it only needs to be stable, not pre-existing).
func DefaultEASWorkingDir(envs map[string]string) string {
	if configcommon.DetectCIProvider(envs) == configcommon.CIProviderBitrise {
		return bitriseDefaultEASWorkingDir
	}

	if home := envs["HOME"]; home != "" {
		return home + "/build"
	}

	return bitriseDefaultEASWorkingDir
}

// environContains reports whether the KEY=... entry is present in a Go-style
// environ slice. Comparison is case-sensitive (matches Unix env semantics).
func environContains(environ []string, key string) bool {
	prefix := key + "="
	for _, entry := range environ {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}

	return false
}

// environToMap parses a Go-style environ slice into a map for DetectCIProvider
// and DefaultEASWorkingDir, both of which want named lookups rather than a
// linear scan.
func environToMap(environ []string) map[string]string {
	out := make(map[string]string, len(environ))
	for _, entry := range environ {
		if idx := strings.IndexByte(entry, '='); idx > 0 {
			out[entry[:idx]] = entry[idx+1:]
		}
	}

	return out
}
