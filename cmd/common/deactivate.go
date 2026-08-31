package common

import (
	"github.com/spf13/cobra"
)

// DeactivateCmd is the parent of all per-tool `deactivate ...` subcommands.
// The subcommands are registered by each tool's cmd package (mirroring
// how ActivateCmd is composed).
//
// Limitations: this flow does NOT clear CI env vars set at activation time
// (via envman / GITHUB_ENV / shell RC files other than the Xcelerate PATH block).
// On CI you may need to restart the workflow or reset the affected variables
// yourself. It also does NOT remove `~/.gradle/init.d/bitrise-gradle-mirrors.init.gradle.kts`
// — that is tracked as a follow-up.
var DeactivateCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "deactivate",
	Short: "Deactivate Bitrise Build Cache for a given tool",
	Long: `Deactivate Bitrise Build Cache for Gradle, Bazel, ccache, Xcode, or React Native.

Call the subcommands with the name of the tool you want to deactivate.
Use ` + "`deactivate all`" + ` to fan out to every supported tool.

Limitations:

- CI env vars set via envman / GITHUB_ENV during activation are NOT cleared.
  Restart the workflow or reset them manually if you need a clean environment.
- ` + "`~/.gradle/init.d/bitrise-gradle-mirrors.init.gradle.kts`" + ` is NOT removed
  (Gradle mirrors cleanup is tracked as a follow-up).`,
}

func init() {
	RootCmd.AddCommand(DeactivateCmd)
}
