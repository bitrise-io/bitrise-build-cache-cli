package gradle

import (
	"fmt"

	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"

	"github.com/bitrise-io/bitrise-build-cache-cli/cmd/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/diagnostics"
)

var restoreGradleOutputDataCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "restore-gradle-output-data",
	Short: "Restore Gradle output data from cache, for running diagnostics builds",
	Long: `Restore Gradle output data from cache, for running diagnostics builds.

This command will:
- Restore the Gradle output data from the Bitrise key-value cache.
`,
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(common.IsDebugLogMode)
		logger.TInfof("Restore Gradle output data from cache, for running diagnostics builds")

		return restoreGradleOutputDataCmdFn(logger)
	},
}

func init() {
	common.RootCmd.AddCommand(restoreGradleOutputDataCmd)
}

func restoreGradleOutputDataCmdFn(logger log.Logger) error {
	envRepo := env.NewRepository()
	commandFactory := command.NewFactory(envRepo)

	restorer := diagnostics.NewGradleDiagnosticOuptutRestorer(
		logger,
		commandFactory,
		envRepo,
	)

	foundRestoredData, err := restorer.Run(common.IsDebugLogMode)
	if err != nil {
		return fmt.Errorf("restore Gradle output: %w", err)
	}

	if foundRestoredData {
		logger.TInfof("✅ Gradle output data restored from cache")
	} else {
		logger.TWarnf("Gradle output data wasn't found. Please ensure that you also run bitrise-build-cache save-gradle-output-data in the build and run at least two builds.")
	}

	return nil
}
