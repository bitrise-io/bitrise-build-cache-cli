package bazelcredhelper

import (
	"os"

	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/bazel"
)

// liteActivation reports whether the bazelrc in force was written by a warmup
// run. There, no credential is the ordinary state of a workspace without Build
// Cache, so the build proceeds uncached instead of being told to see a doctor.
//
// Fail-closed on any error: a machine we cannot prove was warmed up keeps the
// loud message, which is the one a misconfigured developer needs.
func liteActivation() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	sidecar, found, err := bazelconfig.ReadSidecar(home)
	if err != nil || !found {
		return false
	}

	return sidecar.Lite
}
