package cmd

import (
	"fmt"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/diagnostics"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/spf13/cobra"
)

var saveGradleOutputDataCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "save-gradle-output-data",
	Short: "Save Gradle output data to cache, for running diagnostics builds",
	Long: `Save Gradle output data to cache, for running diagnostics builds.

	This command will:
- Collect the contents of **/build/ + .gradle/ directories.
- Save the collected data to the Bitrise key-value cache.
`,
	RunE: func(_ *cobra.Command, _ []string) error {
		logger := log.NewLogger()
		logger.EnableDebugLog(isDebugLogMode)
		logger.TInfof("Save Gradle output data to cache, for running diagnostics builds")

		return saveGradleOutputDataCmdFn(logger)
	},
}

func init() {
	rootCmd.AddCommand(saveGradleOutputDataCmd)
}

func saveGradleOutputDataCmdFn(logger log.Logger) error {
	pathChecker := pathutil.NewPathChecker()
	pathProvider := pathutil.NewPathProvider()
	pathModifier := pathutil.NewPathModifier()
	envRepo := env.NewRepository()

	saveGradleDiagnosticOutputStep := diagnostics.NewGradleDiagnosticOuptutSaver(logger, pathChecker, pathProvider, pathModifier, envRepo)

	if err := saveGradleDiagnosticOutputStep.Run(isDebugLogMode); err != nil {
		return fmt.Errorf("failed to save Gradle output: %w", err)
	}

	logger.TInfof("✅ Gradle output data saved to cache")

	return nil
}
