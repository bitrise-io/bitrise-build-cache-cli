package gradle

import (
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/cmd/common"
	gradlepkg "github.com/bitrise-io/bitrise-build-cache-cli/v3/pkg/gradle"
)

//nolint:gochecknoglobals
var deactivateGradleDryRun bool

var DeactivateGradleCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "gradle",
	Short: "Deactivate Bitrise Build Cache for Gradle",
	Long: `Deactivate Bitrise Build Cache for Gradle.
This command will:

- Remove ~/.gradle/init.d/bitrise-build-cache.init.gradle.kts.
- Strip the "# [start/end] generated-by-bitrise-build-cache" block from ~/.gradle/gradle.properties.
- Remove ~/.bitrise/cache/gradle/config.json.

The gradle.properties file itself is preserved. If nothing was activated the
command reports "already absent" for each step and returns success.

Note: ~/.gradle/init.d/bitrise-gradle-mirrors.init.gradle.kts is NOT removed
(Gradle mirrors cleanup is tracked as a follow-up).`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger := log.NewLogger(log.WithDebugLog(common.IsDebugLogMode))
		logger.EnableDebugLog(common.IsDebugLogMode)
		logger.TInfof("Deactivate Bitrise Build Cache for Gradle")

		return gradlepkg.NewDeactivator(gradlepkg.DeactivatorParams{
			DryRun: deactivateGradleDryRun,
			Logger: logger,
		}).Deactivate(cmd.Context())
	},
}

func init() {
	common.DeactivateCmd.AddCommand(DeactivateGradleCmd)
	DeactivateGradleCmd.Flags().BoolVar(&deactivateGradleDryRun, "dry-run", false, "List intended removals without executing them.")
}
