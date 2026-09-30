package bazelcredhelper

import (
	"os"

	bazelconfig "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/bazel"
)

// liteActivation reports whether the bazelrc in force was written by a preboot
// run. There, no credential is the ordinary state of a workspace without Build
// Cache, so the build proceeds uncached instead of being told to see a doctor.
//
// The marker is read out of the bazelrc itself: a signal kept in a separate
// file could be missing while the bazelrc that needs it is on disk, and then
// every credential-less build on the VM would hard-fail.
//
// Fail-closed on any error: a machine we cannot prove was warmed up keeps the
// loud message, which is the one a misconfigured developer needs.
func liteActivation() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	return bazelconfig.IsLiteBazelrc(home)
}
