package cmd

import (
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/cache"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"
)

var restoreGradleOutputDataCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "restore-gradle-output-data",
	Short: "Restore Gradle output data from cache, for running diagnostics builds",
	Long: `Restore Gradle output data from cache, for running diagnostics builds.

This command will:
- Restore the Gradle output data from the Bitrise key-value cache.
`,
	RunE: func(_ *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof("Restore Gradle output data from cache, for running diagnostics builds")

		return restoreGradleOutputDataCmdFn(logger)
	},
}

func init() {
	rootCmd.AddCommand(restoreGradleOutputDataCmd)
}

func restoreGradleOutputDataCmdFn(logger log.Logger) error {
	envRepo := env.NewRepository()
	commandFactory := command.NewFactory(envRepo)

	restorer := cache.NewGradleDiagnosticOuptutRestorer(
		logger,
		commandFactory,
		envRepo,
	)

	foundRestoredData, err := restorer.Run(isDebugLogMode)
	if err != nil {
		return fmt.Errorf("failed to restore Gradle output: %w", err)
	}

	if foundRestoredData {
		logger.TInfof("✅ Gradle output data restored from cache")
	} else {
		logger.TWarnf("Gradle output data wasn't found. Please ensure that you also run bitrise-build-cache save-gradle-output-data in the build and run at least two builds.")
	}

	return nil
}
