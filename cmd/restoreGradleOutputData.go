package cmd

import (
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/cache"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/spf13/cobra"
)

var restoreGradleOutputDataCmd = &cobra.Command{
	Use:   "restore-gradle-output-data",
	Short: "Restore Gradle output data from cache, for running diagnostics builds",
	Long: `Restore Gradle output data from cache, for running diagnostics builds.

This command will:
- Restore the Gradle output data from the Bitrise key-value cache.
`,
	RunE: func(cmd *cobra.Command, args []string) error {
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

	restoreCacheStep := cache.NewGradleDiagnosticOuptutRestorer(
		logger,
		commandFactory,
		envRepo,
	)

	return restoreCacheStep.Run(isDebugLogMode)
}
